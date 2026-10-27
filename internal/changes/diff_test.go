package changes

import (
	"strings"
	"testing"
)

func TestCountHunks(t *testing.T) {
	for _, test := range []struct {
		name, diff       string
		added, removed   int
		complete, binary bool
	}{
		{"literal header prefixes inside a hunk", "--- a/file\n+++ b/file\n@@ -1,2 +1,2 @@\n--- actual old content\n+++ actual new content\n same\n", 1, 1, true, false},
		{"multiple hunks", "@@ -1 +1,2 @@\n-old\n+new\n+extra\n@@ -9 +10 @@\n-stale\n+fresh\n\\ No newline at end of file\n", 3, 2, true, false},
		{"clipped hunk", "@@ -1,2 +1,2 @@\n-old\n+new\n", 1, 1, false, false},
		{"bare engine patch", "+one\n+two\n-old\n", 2, 1, true, false},
		{"ambiguous headerless prefixes", "--- source text\n+++ source text\n", 1, 1, false, false},
		{"binary", "diff --git a/a b/a\nBinary files a/a and b/a differ\n", 0, 0, false, true},
		{"unexpected text", "not a patch", 0, 0, false, false},
		{"empty", "", 0, 0, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Count(test.diff)
			if got.Additions != test.added || got.Deletions != test.removed || got.StatsComplete != test.complete || got.Binary != test.binary {
				t.Fatalf("stats = %+v", got)
			}
		})
	}
}

func TestTurnPathsAndOffsets(t *testing.T) {
	sections := []string{
		"diff --git a/new file.txt b/new file.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new file.txt\n@@ -0,0 +1 @@\n+diff --git a/not-a-file b/not-a-file\n",
		"diff --git a/old.txt b/old.txt\ndeleted file mode 100644\n--- a/old.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n",
		"diff --git \"a/old\\tname\" \"b/new\\tname\"\nsimilarity index 100%\nrename from \"old\\tname\"\nrename to \"new\\tname\"\n",
		"diff --git a/a b/name b/a b/name\nold mode 100644\nnew mode 100755\n",
		"diff --git a/source b/copied\nsimilarity index 100%\ncopy from source\ncopy to copied\n",
	}
	diff := strings.Join(sections, "")
	files, complete := ParseTurn(diff)
	if !complete || len(files) != 5 {
		t.Fatalf("files=%+v complete=%v", files, complete)
	}
	wantPaths := []string{"new file.txt", "old.txt", "old\tname", "a b/name", "copied"}
	for index, file := range files {
		if file.Path != wantPaths[index] || diff[file.Start:file.End] != sections[index] || !file.StatsComplete {
			t.Fatalf("file %d = %+v", index, file)
		}
	}
	if files[0].Kind != "add" || files[0].Additions != 1 || files[1].Kind != "delete" || files[1].Deletions != 1 || files[2].MovePath != "new\tname" {
		t.Fatalf("file metadata = %+v", files)
	}
	if files[4].Kind != "add" || files[4].MovePath != "" {
		t.Fatalf("copy treated as rename: %+v", files[4])
	}
}

func TestTurnUnrecognizedInputDoesNotClaimComplete(t *testing.T) {
	if _, complete := ParseTurn("@@ -1 +1 @@\n-old\n+new\n"); complete {
		t.Fatal("headerless turn cannot provide a complete file list")
	}
	files, complete := ParseTurn("diff --git \"a/\\346\\265\\213\\350\\257\\225\" \"b/\\346\\265\\213\\350\\257\\225\"\nold mode 100644\nnew mode 100755\n")
	if !complete || len(files) != 1 || files[0].Path != "测试" {
		t.Fatalf("quoted UTF-8 path = %+v complete=%v", files, complete)
	}
}
