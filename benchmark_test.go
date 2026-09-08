package chaintrail

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkCanonicalJSON(b *testing.B) {
	payload := []byte(`{"service":"api","attempt":3.0,"labels":{"region":"us-east-1","tier":"prod"},"durations":[1.20,0.003,99]}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := canonicalJSON(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerify1000Records(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "journal")
	j := Open(dir)
	if _, err := j.Init(); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if _, err := j.Append("benchmark.event", []byte(fmt.Sprintf(`{"n":%d,"ok":true}`, i))); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := j.Verify(VerifyOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
