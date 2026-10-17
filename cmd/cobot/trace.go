package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/wiretrace"
)

const traceUsage = `cobot trace — developer recording of everything CoBot exchanges

Usage:
  cobot trace on       [--profile p | --data-root dir]   start recording (no restart needed)
  cobot trace off      [--profile p | --data-root dir]   stop; adds Codex's own records
  cobot trace status   [--profile p | --data-root dir]
  cobot trace protocol [--write-baseline file]           compare this Codex's protocol with CoBot's baseline

Recordings go to <data root>/traces/<start time>/, one JSONL file per process.
`

func runTrace(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, traceUsage)
		return 2
	}
	flags := flag.NewFlagSet("trace", flag.ContinueOnError)
	profile := flags.String("profile", string(platform.EnvDevelopment), "data profile: development or production")
	dataRoot := flags.String("data-root", "", "explicit data root")
	writeBaseline := flags.String("write-baseline", "", "protocol: write this Codex's protocol listing to the file")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	env, err := platform.ParseEnvironment(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths, err := platform.Resolve(env, *dataRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "on":
		session, err := wiretrace.Start(paths.Root, time.Now())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("recording on: %s\n", session.Dir(paths.Root))
		fmt.Println("running CoBot processes pick it up within a second; engines started from now on also log their debug detail")
	case "off":
		session, err := wiretrace.Stop(paths.Root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		copied := copyEngineRecords(paths, session)
		fmt.Printf("recording off: %s\n", session.Dir(paths.Root))
		fmt.Printf("added %d file(s) Codex wrote meanwhile under codex-home/\n", copied)
		printSessionFiles(session.Dir(paths.Root))
	case "status":
		session, ok := wiretrace.ActiveSession(paths.Root)
		if !ok {
			fmt.Println("recording off")
			return 0
		}
		fmt.Printf("recording on since %s: %s\n", session.StartedAt.Format(time.DateTime), session.Dir(paths.Root))
		printSessionFiles(session.Dir(paths.Root))
	case "protocol":
		return runTraceProtocol(paths, *writeBaseline)
	default:
		fmt.Fprint(os.Stderr, traceUsage)
		return 2
	}
	return 0
}

// copyEngineRecords copies what Codex itself wrote during the recording
// (session rollouts, logs, state) next to CoBot's records, except the login.
func copyEngineRecords(paths platform.Paths, session wiretrace.Session) int {
	copied := 0
	for name, root := range map[string]string{"home": paths.CodexHome(), "sqlite": paths.CodexSQLiteDir()} {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || entry.Name() == "auth.json" {
				return nil
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().Before(session.StartedAt) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			target := filepath.Join(session.Dir(paths.Root), "codex-home", name, rel)
			if copyFile(path, target) == nil {
				copied++
			}
			return nil
		})
	}
	return copied
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func printSessionFiles(dir string) {
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			rel, _ := filepath.Rel(dir, path)
			fmt.Printf("  %-60s %10d bytes\n", rel, info.Size())
		}
		return nil
	})
}

// runTraceProtocol lists the installed Codex's protocol and either stores it
// as the new baseline or reports how it differs from the current one.
func runTraceProtocol(paths platform.Paths, writeBaseline string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary, err := codex.Locate("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex not found:", err)
		return 1
	}
	version, _ := codex.ProbeVersion(ctx, binary)
	current, err := codex.ExportProtocol(ctx, binary, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if writeBaseline != "" {
		data, err := json.MarshalIndent(current, "", " ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(writeBaseline, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("wrote the protocol of codex %s (%d facts) to %s\n", version, len(current.Facts), writeBaseline)
		return 0
	}
	baseline, err := codex.Baseline()
	if err != nil {
		fmt.Fprintln(os.Stderr, "read baseline:", err)
		return 1
	}
	diff := codex.DiffProtocol(baseline, current)
	report := protocolReport(baseline, current, diff)
	dir := wiretrace.TracesDir(paths.Root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	file := filepath.Join(dir, fmt.Sprintf("protocol-%s-%s.md", safeName(version), time.Now().Format("2006-01-02_15-04-05")))
	if err := os.WriteFile(file, []byte(report), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("baseline codex %s → installed codex %s: %d added, %d removed\n", baseline.CodexVersion, version, len(diff.Added), len(diff.Removed))
	fmt.Println("report:", file)
	return 0
}

func safeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, s)
}

// protocolReport writes the comparison for a person: message types that
// appeared or went away first, then every other change by definition.
func protocolReport(baseline, current codex.ProtocolFacts, diff codex.ProtocolDiff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Codex 协议对比\n\n基线：codex %s（CoBot 按这个版本适配）  \n当前：codex %s  \n生成时间：%s\n\n",
		baseline.CodexVersion, current.CodexVersion, time.Now().Format(time.DateTime))
	fmt.Fprintf(&b, "共新增 %d 条、删除 %d 条。每一条是协议里的一个事实：消息、字段及类型、枚举值或联合类型的一个分支。\n\n", len(diff.Added), len(diff.Removed))
	if len(diff.Added) == 0 && len(diff.Removed) == 0 {
		b.WriteString("协议没有变化。\n")
		return b.String()
	}
	methods := func(title string, facts []string) {
		var lines []string
		for _, fact := range facts {
			if union, method, ok := codex.IsMethodFact(fact); ok {
				lines = append(lines, fmt.Sprintf("- `%s`（%s）", method, union))
			}
		}
		fmt.Fprintf(&b, "## %s（%d）\n\n", title, len(lines))
		if len(lines) == 0 {
			b.WriteString("无\n\n")
			return
		}
		b.WriteString(strings.Join(lines, "\n") + "\n\n")
	}
	methods("新增的消息类型", diff.Added)
	methods("删除的消息类型", diff.Removed)

	groups := map[string][]string{}
	group := func(sign string, facts []string) {
		for _, fact := range facts {
			name := fact
			if i := strings.IndexAny(fact, ".|[{ "); i > 0 {
				name = fact[:i]
			}
			groups[name] = append(groups[name], sign+" "+fact)
		}
	}
	group("+", diff.Added)
	group("-", diff.Removed)
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "## 全部变化（按定义分组，%d 个定义）\n\n", len(names))
	for _, name := range names {
		fmt.Fprintf(&b, "### %s\n\n```diff\n%s\n```\n\n", name, strings.Join(groups[name], "\n"))
	}
	return b.String()
}
