// Package architecture holds the module boundaries of the backend to account.
// The rules are in docs/rules/architecture.md. What already breaks a rule is
// listed in baseline.json, which may only shrink.
package architecture

import (
	"encoding/json"
	"flag"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	modulePath = "github.com/kunghim/CoBot/"
	appPkg     = modulePath + "internal/app"
	corePkg    = appPkg + "/core"
	storePkg   = modulePath + "internal/store/sqlite"
	codexPkg   = modulePath + "internal/engine/codex"
)

var update = flag.Bool("update-baseline", false, "rewrite the current violations into baseline.json (keeps reasons)")

// finding is one violation: where it is and what it reaches. Line numbers are
// left out so an unrelated edit does not disturb the baseline.
type finding struct {
	Rule   string `json:"rule"`
	File   string `json:"file"`
	Func   string `json:"func"`
	Target string `json:"target,omitempty"`
	// Reason says why a listed violation is not a real one, when it is not.
	Reason string `json:"reason,omitempty"`
}

func (f finding) key() string { return f.Rule + "|" + f.File + "|" + f.Func + "|" + f.Target }

var (
	loadOnce sync.Once
	loaded   []*packages.Package
	loadErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func load(t *testing.T) []*packages.Package {
	t.Helper()
	loadOnce.Do(func() {
		loaded, loadErr = packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
				packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
			Dir: repoRoot(t),
		}, "./internal/...")
	})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, p := range loaded {
		for _, e := range p.Errors {
			t.Fatalf("%s: %v", p.PkgPath, e)
		}
	}
	return loaded
}

// appPackages are internal/app and the modules moved out of it.
func appPackages(t *testing.T) []*packages.Package {
	t.Helper()
	var out []*packages.Package
	for _, p := range load(t) {
		if p.PkgPath == appPkg || strings.HasPrefix(p.PkgPath, appPkg+"/") {
			out = append(out, p)
		}
	}
	return out
}

func inAppPackages(fn *types.Func) bool {
	return fn != nil && fn.Pkg() != nil && (fn.Pkg().Path() == appPkg || strings.HasPrefix(fn.Pkg().Path(), appPkg+"/"))
}

func appPackage(t *testing.T) *packages.Package {
	t.Helper()
	for _, p := range load(t) {
		if p.PkgPath == appPkg {
			return p
		}
	}
	t.Fatal("internal/app not loaded")
	return nil
}

// relative names a source file from the repository root.
func relative(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot(t), path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// eachFunc visits every top-level function of a package, with its file.
func eachFunc(t *testing.T, p *packages.Package, visit func(file string, fn *ast.FuncDecl)) {
	t.Helper()
	for _, f := range p.Syntax {
		file := relative(t, p.Fset.Position(f.Pos()).Filename)
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				visit(file, fn)
			}
		}
	}
}

// calleeOf is the function a call statically reaches, generics resolved to
// their declaration. Calls through interfaces and function values have none.
func calleeOf(info *types.Info, call *ast.CallExpr) *types.Func {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return nil
	}
	return fn.Origin()
}

func inPackage(fn *types.Func, path string) bool {
	return fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == path
}

// check compares a rule's findings with the baseline: anything new fails, and
// so does an entry that no longer happens, so the list can only shrink.
func check(t *testing.T, rule string, found []finding) {
	t.Helper()
	path := filepath.Join("baseline.json")
	var baseline []finding
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &baseline); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if *update {
		writeBaseline(t, path, baseline, rule, found)
		return
	}
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.key()] = true
	}
	known := map[string]bool{}
	sources := loadedFiles(t)
	for _, b := range baseline {
		if b.Rule != rule {
			continue
		}
		known[b.key()] = true
		// A platform's own source is only read on that platform.
		if !seen[b.key()] && sources[b.File] {
			t.Errorf("no longer happens, remove it from baseline.json: %s", b.key())
		}
	}
	for _, f := range found {
		if !known[f.key()] {
			t.Errorf("new violation of %s: %s in %s reaches %s (see docs/rules/architecture.md)", rule, f.File, f.Func, f.Target)
		}
	}
}

func loadedFiles(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, p := range load(t) {
		for _, f := range p.CompiledGoFiles {
			out[relative(t, f)] = true
		}
	}
	return out
}

func writeBaseline(t *testing.T, path string, baseline []finding, rule string, found []finding) {
	t.Helper()
	reasons := map[string]string{}
	kept := baseline[:0:0]
	sources := loadedFiles(t)
	for _, b := range baseline {
		if b.Rule != rule {
			kept = append(kept, b)
			continue
		}
		reasons[b.key()] = b.Reason
		if !sources[b.File] {
			kept = append(kept, b)
		}
	}
	for _, f := range found {
		f.Reason = reasons[f.key()]
		kept = append(kept, f)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].key() < kept[j].key() })
	kept = slices.CompactFunc(kept, func(a, b finding) bool { return a.key() == b.key() })
	data, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

type manifest struct {
	Layers  []string `json:"layers"`
	Modules map[string]struct {
		Layer string   `json:"layer"`
		Files []string `json:"files"`
	} `json:"modules"`
}

func readManifest(t *testing.T) manifest {
	t.Helper()
	data, err := os.ReadFile("modules.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestModuleManifest: every file of internal/app belongs to exactly one
// module, on every platform, and the manifest names no file that is gone.
func TestModuleManifest(t *testing.T) {
	m := readManifest(t)
	declared := map[string]string{}
	for name, module := range m.Modules {
		if !slices.Contains(m.Layers, module.Layer) {
			t.Errorf("module %s has unknown layer %q", name, module.Layer)
		}
		for _, file := range module.Files {
			if other, dup := declared[file]; dup {
				t.Errorf("%s is in both %s and %s", file, other, name)
			}
			declared[file] = name
		}
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "internal", "app"))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			present[name] = true
			if declared[name] == "" {
				t.Errorf("internal/app/%s belongs to no module: add it to tests/architecture/modules.json", name)
			}
		}
	}
	for file := range declared {
		if !present[file] {
			t.Errorf("modules.json names %s, which does not exist", file)
		}
	}
}

// storeCallsOutside lists calls to the named store functions from outside the
// store and outside the one file allowed to make them.
func storeCallsOutside(t *testing.T, rule string, names []string, allowed string) []finding {
	t.Helper()
	var found []finding
	for _, p := range load(t) {
		if p.PkgPath == storePkg {
			continue
		}
		eachFunc(t, p, func(file string, fn *ast.FuncDecl) {
			if file == allowed {
				return
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					callee := calleeOf(p.TypesInfo, call)
					if inPackage(callee, storePkg) && slices.Contains(names, callee.Name()) {
						found = append(found, finding{Rule: rule, File: file, Func: fn.Name.Name, Target: callee.Name()})
					}
				}
				return true
			})
		})
	}
	return dedupe(found)
}

func dedupe(found []finding) []finding {
	sort.Slice(found, func(i, j int) bool { return found[i].key() < found[j].key() })
	return slices.CompactFunc(found, func(a, b finding) bool { return a.key() == b.key() })
}

// TestInboxWriteSites: only the inbox writes messages and deliveries. Deleting
// a team or a member (Purge*) is the deliberate exception, kept in one place in
// the store.
func TestInboxWriteSites(t *testing.T) {
	check(t, "inbox-write", storeCallsOutside(t, "inbox-write", []string{
		"InsertMessage", "UpdateMessageBlocks", "DeleteRunMessages", "DeleteMessageDeliveries", "SetMessageDeliveryStatus",
		"InsertDeliveryRow", "SetDeliveryStatus", "ClaimDeliveryRun", "SetRunDeliveriesStatus", "CancelChainDeliveries",
		"MarkDeliveryUnknownByRun", "CancelQueuedForTask", "WithdrawDelivery",
	}, "internal/app/inbox.go"))
}

// TestMemberStateSites: a member's run state changes in one place.
func TestMemberStateSites(t *testing.T) {
	check(t, "member-state", storeCallsOutside(t, "member-state", []string{"SetBotRunState"}, "internal/app/member_state.go"))
}

var writeSQL = regexp.MustCompile(`(?i)\b(insert\s+(or\s+\w+\s+)?into|update(\s+or\s+\w+)?\s+\w+\s+set|delete\s+from|replace\s+into)\b`)

// TestNoHandWrittenWrites: statements that change data live in the store.
func TestNoHandWrittenWrites(t *testing.T) {
	var found []finding
	for _, p := range appPackages(t) {
		eachFunc(t, p, func(file string, fn *ast.FuncDecl) {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !strings.HasPrefix(sel.Sel.Name, "Exec") {
					return true
				}
				for _, arg := range call.Args {
					if tv := p.TypesInfo.Types[arg]; tv.Value != nil && tv.Value.Kind() == constant.String && writeSQL.MatchString(constant.StringVal(tv.Value)) {
						found = append(found, finding{Rule: "hand-written-write", File: file, Func: fn.Name.Name, Target: tableOf(constant.StringVal(tv.Value))})
					}
				}
				return true
			})
		})
	}
	check(t, "hand-written-write", dedupe(found))
}

var writeTarget = regexp.MustCompile(`(?i)(?:into|update|from)\s+(\w+)`)

func tableOf(statement string) string {
	if m := writeTarget.FindStringSubmatch(statement); m != nil {
		return strings.ToLower(m[1])
	}
	return "?"
}

// appGraph knows, for the functions of internal/app, what each calls.
// appGraph covers internal/app and the modules moved out of it as one program.
type appGraph struct {
	pkgs   []*packages.Package
	info   *types.Info
	decls  map[*types.Func]*ast.FuncDecl
	calls  map[*types.Func][]*types.Func
	fields map[*types.Var]*types.Func
}

func newAppGraph(t *testing.T) *appGraph {
	t.Helper()
	g := &appGraph{
		pkgs: appPackages(t), decls: map[*types.Func]*ast.FuncDecl{}, calls: map[*types.Func][]*types.Func{}, fields: map[*types.Var]*types.Func{},
		info: &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Instances: map[*ast.Ident]types.Instance{}},
	}
	for _, p := range g.pkgs {
		maps.Copy(g.info.Types, p.TypesInfo.Types)
		maps.Copy(g.info.Defs, p.TypesInfo.Defs)
		maps.Copy(g.info.Uses, p.TypesInfo.Uses)
		maps.Copy(g.info.Selections, p.TypesInfo.Selections)
		maps.Copy(g.info.Instances, p.TypesInfo.Instances)
	}
	tables := g.tableTargets()
	g.eachFunc(t, func(_ string, fn *ast.FuncDecl) { g.noteHandedIn(fn) })
	g.eachFunc(t, func(_ string, fn *ast.FuncDecl) {
		obj, _ := g.info.Defs[fn.Name].(*types.Func)
		if obj == nil {
			return
		}
		g.decls[obj] = fn
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if callee := g.callee(call); inAppPackages(callee) {
					g.calls[obj] = append(g.calls[obj], callee)
				}
			}
			// Reading a table can reach whatever its rows name.
			if id, ok := n.(*ast.Ident); ok {
				if table, ok := g.info.Uses[id].(*types.Var); ok {
					g.calls[obj] = append(g.calls[obj], tables[table]...)
				}
			}
			return true
		})
	})
	return g
}

func (g *appGraph) eachFunc(t *testing.T, visit func(file string, fn *ast.FuncDecl)) {
	t.Helper()
	for _, p := range g.pkgs {
		eachFunc(t, p, visit)
	}
}

// noteHandedIn remembers which function a struct field was given in a literal
// (`core.Core{ValidateToolRun: a.ValidateToolRun}`): the moved modules call back
// into internal/app that way, and calling the field reaches that function.
func (g *appGraph) noteHandedIn(fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		pair, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			return true
		}
		field, ok := g.info.Uses[key].(*types.Var)
		if !ok || !field.IsField() {
			return true
		}
		if sel, ok := pair.Value.(*ast.SelectorExpr); ok {
			if method, ok := g.info.Uses[sel.Sel].(*types.Func); ok && inAppPackages(method) {
				g.fields[field] = method
			}
		}
		return true
	})
}

// callee is the function a call reaches, a field handed a function included.
func (g *appGraph) callee(call *ast.CallExpr) *types.Func {
	if fn := calleeOf(g.info, call); fn != nil {
		return fn
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if field, ok := g.info.Uses[sel.Sel].(*types.Var); ok {
			return g.fields[field]
		}
	}
	return nil
}

// tableTargets lists, for each package-level variable, the functions its
// initial value names: a table's rows call them through a function value, which
// no call expression shows.
func (g *appGraph) tableTargets() map[*types.Var][]*types.Func {
	out := map[*types.Var][]*types.Func{}
	for _, p := range g.pkgs {
		for _, init := range p.TypesInfo.InitOrder {
			for _, lhs := range init.Lhs {
				ast.Inspect(init.Rhs, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						if fn, ok := g.info.Uses[id].(*types.Func); ok && inAppPackages(fn) {
							out[lhs] = append(out[lhs], fn.Origin())
						}
					}
					return true
				})
			}
		}
	}
	return out
}

// reaching is the set of functions from which a marked function can be
// reached, the marked ones included.
func (g *appGraph) reaching(marked map[*types.Func]bool) map[*types.Func]bool {
	out := map[*types.Func]bool{}
	for f := range marked {
		out[f] = true
	}
	for changed := true; changed; {
		changed = false
		for f, callees := range g.calls {
			if out[f] {
				continue
			}
			for _, c := range callees {
				if out[c] {
					out[f] = true
					changed = true
					break
				}
			}
		}
	}
	return out
}

// isPoolField reports the App's raw connection pool: a selection of its DB field.
func (g *appGraph) isPoolField(n ast.Node) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "DB" {
		return false
	}
	selection := g.info.Selections[sel]
	return selection != nil && selection.Kind() == types.FieldVal
}

// touchesPool are the functions that may reach the pool, directly or through
// the calls the graph can see.
func (g *appGraph) touchesPool() map[*types.Func]bool {
	direct := map[*types.Func]bool{}
	for obj, fn := range g.decls {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// Handing the pool to a struct (`core.Core{DB: a.DB}`) uses nothing;
			// whatever reads that field later counts where it does.
			if pair, ok := n.(*ast.KeyValueExpr); ok && g.isPoolField(pair.Value) {
				return false
			}
			if g.isPoolField(n) {
				direct[obj] = true
			}
			return true
		})
	}
	return g.reaching(direct)
}

// TestNoDBInsideTransaction: the pool has one connection, so anything that
// reaches it while this goroutine holds a transaction waits on itself until
// the context ends. Follows the statements of each function with the
// transaction open or not; a function handed a *sql.Tx is open throughout.
// Calls through interfaces and function values are not seen, except that reading
// a table counts as reaching what its rows name.
func TestNoDBInsideTransaction(t *testing.T) {
	g := newAppGraph(t)
	touching := g.touchesPool()
	var found []finding
	g.eachFunc(t, func(file string, fn *ast.FuncDecl) {
		w := &txWalker{g: g, touching: touching, report: func(target string) {
			found = append(found, finding{Rule: "db-inside-tx", File: file, Func: fn.Name.Name, Target: target})
		}}
		w.block(fn.Body.List, takesTx(g.info, fn))
	})
	check(t, "db-inside-tx", dedupe(found))
}

func takesTx(info *types.Info, fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		if isTxType(info.TypeOf(field.Type)) {
			return true
		}
	}
	return false
}

func isTxType(typ types.Type) bool {
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "Tx" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "database/sql"
}

type txWalker struct {
	g        *appGraph
	touching map[*types.Func]bool
	report   func(target string)
}

// block walks statements in order and returns whether a transaction is still
// open after them. A branch that leaves (return, break, continue, panic) does
// not change the state after it.
func (w *txWalker) block(list []ast.Stmt, open bool) bool {
	for _, s := range list {
		open = w.stmt(s, open)
	}
	return open
}

func leaves(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	switch last := list[len(list)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if call, ok := last.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	}
	return false
}

func (w *txWalker) branch(list []ast.Stmt, open bool) bool {
	after := w.block(list, open)
	if leaves(list) {
		return open
	}
	return after
}

func (w *txWalker) stmt(s ast.Stmt, open bool) bool {
	switch s := s.(type) {
	case *ast.BlockStmt:
		return w.block(s.List, open)
	case *ast.IfStmt:
		if s.Init != nil {
			open = w.stmt(s.Init, open)
		}
		open = w.expr(s.Cond, open)
		then := w.branch(s.Body.List, open)
		other := open
		if s.Else != nil {
			if block, ok := s.Else.(*ast.BlockStmt); ok {
				other = w.branch(block.List, open)
			} else {
				other = w.stmt(s.Else, open)
			}
		}
		// `if tx != nil { tx.Rollback() }`: without a transaction there is nothing
		// to leave open, so only the branch that had one counts.
		if s.Else == nil && w.g.guardsTx(s.Cond) {
			return then
		}
		// Open after the if when any way through leaves it open.
		return then || other
	case *ast.ForStmt:
		if s.Init != nil {
			open = w.stmt(s.Init, open)
		}
		return w.branch(s.Body.List, open) || open
	case *ast.RangeStmt:
		open = w.expr(s.X, open)
		return w.branch(s.Body.List, open) || open
	case *ast.SwitchStmt:
		if s.Init != nil {
			open = w.stmt(s.Init, open)
		}
		if s.Tag != nil {
			open = w.expr(s.Tag, open)
		}
		return w.clauses(s.Body, open)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			open = w.stmt(s.Init, open)
		}
		return w.clauses(s.Body, open)
	case *ast.SelectStmt:
		return w.clauses(s.Body, open)
	case *ast.DeferStmt, *ast.GoStmt:
		// Runs later or elsewhere: `defer tx.Rollback()` ends nothing here.
		return open
	case *ast.LabeledStmt:
		return w.stmt(s.Stmt, open)
	}
	// Plain statements: assignments, expressions, returns, declarations.
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			w.block(n.Body.List, open)
			return false
		case ast.Expr:
			open = w.expr(n, open)
			return false
		}
		return true
	})
	return open
}

func (w *txWalker) clauses(body *ast.BlockStmt, open bool) bool {
	after := open
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			list = c.Body
		case *ast.CommClause:
			if c.Comm != nil {
				open = w.stmt(c.Comm, open)
			}
			list = c.Body
		}
		after = w.branch(list, open) || after
	}
	return after
}

// expr scans one expression in evaluation order: arguments before the call.
func (w *txWalker) expr(e ast.Expr, open bool) bool {
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			w.block(n.Body.List, open)
			return false
		case *ast.CallExpr:
			for _, arg := range n.Args {
				open = w.expr(arg, open)
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
				open = w.expr(sel.X, open)
				switch w.g.txMethod(sel) {
				case "begin":
					return false
				case "end":
					open = false
					return false
				}
			}
			if open {
				if callee := w.g.callee(n); callee != nil && w.touching[callee] {
					w.report(callee.Name())
				}
			}
			return false
		case *ast.SelectorExpr:
			if open && w.g.isPoolField(n) {
				w.report("DB")
			}
			return true
		}
		return true
	})
	if e != nil && w.g.beginsTx(e) {
		return true
	}
	return open
}

// txMethod classifies database/sql calls that open or close a transaction.
func (g *appGraph) txMethod(sel *ast.SelectorExpr) string {
	switch sel.Sel.Name {
	case "BeginTx", "Begin":
		if isSQLReceiver(g.info.TypeOf(sel.X), "DB") {
			return "begin"
		}
	case "Commit", "Rollback":
		if isSQLReceiver(g.info.TypeOf(sel.X), "Tx") {
			return "end"
		}
	}
	return ""
}

func isSQLReceiver(typ types.Type, name string) bool {
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == name && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "database/sql"
}

// guardsTx recognizes the condition `tx != nil`.
func (g *appGraph) guardsTx(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	nilIdent, ok := bin.Y.(*ast.Ident)
	return ok && nilIdent.Name == "nil" && isTxType(g.info.TypeOf(bin.X))
}

func (g *appGraph) beginsTx(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && g.txMethod(sel) == "begin"
}

// TestNoNestedBeginWrite: the write lock is not reentrant, so a function
// holding it must not reach another that takes it. A caller that clears its
// principal's write scope first is fine; such pairs are listed with a reason.
func TestNoNestedBeginWrite(t *testing.T) {
	g := newAppGraph(t)
	begins := func(c *types.Func) bool {
		return (c.Name() == "beginWrite" && inPackage(c, appPkg)) || (c.Name() == "Begin" && inPackage(c, corePkg))
	}
	takes := map[*types.Func]bool{}
	for obj, callees := range g.calls {
		for _, c := range callees {
			if begins(c) && !begins(obj) {
				takes[obj] = true
			}
		}
	}
	var found []finding
	g.eachFunc(t, func(file string, fn *ast.FuncDecl) {
		obj, _ := g.info.Defs[fn.Name].(*types.Func)
		if obj == nil || !takes[obj] {
			return
		}
		seen := map[*types.Func]bool{obj: true}
		stack := append([]*types.Func(nil), g.calls[obj]...)
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[c] {
				continue
			}
			seen[c] = true
			if takes[c] {
				found = append(found, finding{Rule: "nested-begin-write", File: file, Func: fn.Name.Name, Target: c.Name()})
			}
			stack = append(stack, g.calls[c]...)
		}
	})
	check(t, "nested-begin-write", dedupe(found))
}

// TestCodexConfinement: Codex's own types and configuration names stay behind
// the engine interface. Only the composition root and the command line may
// import the adapter.
func TestCodexConfinement(t *testing.T) {
	var found []finding
	allowed := []string{modulePath + "internal/bootstrap", modulePath + "internal/cli", codexPkg}
	for _, p := range load(t) {
		if slices.Contains(allowed, p.PkgPath) {
			continue
		}
		for _, f := range p.Syntax {
			file := relative(t, p.Fset.Position(f.Pos()).Filename)
			for _, imp := range f.Imports {
				if strings.Trim(imp.Path.Value, `"`) == codexPkg {
					found = append(found, finding{Rule: "codex-import", File: file, Func: "import", Target: "engine/codex"})
				}
			}
		}
	}
	check(t, "codex-import", dedupe(found))

	var keys []finding
	p := appPackage(t)
	eachFunc(t, p, func(file string, fn *ast.FuncDecl) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				value := strings.Trim(lit.Value, "\"`")
				if value == "web_search" || value == "model_context_window" || strings.HasPrefix(value, "mcp_servers") {
					keys = append(keys, finding{Rule: "codex-config-key", File: file, Func: fn.Name.Name, Target: value})
				}
			}
			return true
		})
	})
	check(t, "codex-config-key", dedupe(keys))
}

// TestMovedModules: internal/app imports the modules moved out of it, so they
// cannot reach back; the language holds that. They also stay apart from each
// other: the only one they may build on is core, which builds on none.
func TestMovedModules(t *testing.T) {
	for _, p := range appPackages(t) {
		if p.PkgPath == appPkg {
			continue
		}
		for path := range p.Imports {
			if !strings.HasPrefix(path, appPkg+"/") {
				continue
			}
			if p.PkgPath == corePkg || path != corePkg {
				t.Errorf("%s imports %s: moved modules build on core only", p.PkgPath, path)
			}
		}
	}
}

// TestModuleLayers: inside internal/app, a lower layer does not call a higher
// one. Modules of one layer may call each other for now.
func TestModuleLayers(t *testing.T) {
	m := readManifest(t)
	moduleOf := map[string]string{}
	for name, module := range m.Modules {
		for _, file := range module.Files {
			moduleOf["internal/app/"+file] = name
		}
	}
	rank := func(module string) int { return slices.Index(m.Layers, m.Modules[module].Layer) }
	g := newAppGraph(t)
	var found []finding
	g.eachFunc(t, func(file string, fn *ast.FuncDecl) {
		from, ok := moduleOf[file]
		if !ok {
			return
		}
		obj, _ := g.info.Defs[fn.Name].(*types.Func)
		for _, callee := range g.calls[obj] {
			to, ok := moduleOf[relative(t, g.pkgs[0].Fset.Position(callee.Pos()).Filename)]
			if ok && rank(from) < rank(to) {
				found = append(found, finding{Rule: "layer", File: file, Func: fn.Name.Name, Target: to + "." + callee.Name()})
			}
		}
	})
	check(t, "layer", dedupe(found))
}
