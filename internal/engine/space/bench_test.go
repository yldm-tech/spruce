package space

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// benchTree builds a synthetic home-like tree of about files files: folders of 12 files, 8 folders per level, every 50th file 2 MB (sparse where the file system allows, so the tree costs inodes rather than space).
func benchTree(b *testing.B, files int) string {
	b.Helper()
	root := filepath.Join(tempRoot(b), "home")
	made := 0
	var fill func(dir string, depth int)
	fill = func(dir string, depth int) {
		if made >= files {
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < 12 && made < files; i++ {
			p := filepath.Join(dir, "f"+strconv.Itoa(i)+".dat")
			f, err := os.Create(p)
			if err != nil {
				b.Fatal(err)
			}
			if made%50 == 0 {
				if err := f.Truncate(2 << 20); err != nil {
					b.Fatal(err)
				}
			} else if _, err := f.Write([]byte("spruce")); err != nil {
				b.Fatal(err)
			}
			f.Close()
			made++
		}
		for i := 0; i < 8 && made < files && depth < 12; i++ {
			fill(filepath.Join(dir, "d"+strconv.Itoa(i)), depth+1)
		}
	}
	for top := 0; made < files; top++ {
		fill(filepath.Join(root, "top"+strconv.Itoa(top)), 0)
	}
	return root
}

// BenchmarkScan scans a synthetic tree of SPRUCE_SPACE_BENCH_FILES files (default 100000) and reports files per second and the heap the finished tree holds. Run with: go test -run '^$' -bench Scan -benchtime 3x ./internal/engine/space/
func BenchmarkScan(b *testing.B) {
	files := 100_000
	if v, err := strconv.Atoi(os.Getenv("SPRUCE_SPACE_BENCH_FILES")); err == nil && v > 0 {
		files = v
	}
	start := time.Now()
	root := benchTree(b, files)
	b.Logf("built %d files in %v", files, time.Since(start).Round(time.Millisecond))

	var heapMB, peakMB float64
	var st Stats
	var nodes, large int
	for b.Loop() {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		tree, err := Scan(b.Context(), root, Options{LargeFileMin: 1 << 20})
		if err != nil {
			b.Fatal(err)
		}
		var during runtime.MemStats
		runtime.ReadMemStats(&during)
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		heapMB = float64(after.HeapAlloc-min(before.HeapAlloc, after.HeapAlloc)) / 1e6
		peakMB = max(peakMB, float64(during.HeapSys)/1e6)
		st, nodes, large = tree.Stats(), len(tree.nodes), len(tree.LargeFiles())
		runtime.KeepAlive(tree)
	}
	if st.Files != int64(files) {
		b.Fatalf("scanned %d files of %d", st.Files, files)
	}
	perOp := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(float64(files)/perOp, "files/s")
	b.ReportMetric(heapMB, "tree-MB")
	b.ReportMetric(peakMB, "heap-sys-MB")
	b.Logf("%s: %d files, %d folders, %d nodes kept, %d large files, last walk %v, %.2fs per loop", runtime.GOOS, st.Files, st.Dirs, nodes, large, st.Duration.Round(time.Millisecond), perOp)
}
