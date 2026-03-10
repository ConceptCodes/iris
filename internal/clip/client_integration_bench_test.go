package clip

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
)

var integrationPNGBytes = mustDecodeBenchBase64(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7Z0XQAAAAASUVORK5CYII=",
)

func mustDecodeBenchBase64(input string) []byte {
	data, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		panic(err)
	}
	return data
}

func requireClipBenchClient(b *testing.B) *Client {
	b.Helper()

	target := os.Getenv("CLIP_BENCH_ADDR")
	if target == "" {
		b.Skip("set CLIP_BENCH_ADDR to run CLIP integration benchmarks")
	}

	client, err := NewClient(target)
	if err != nil {
		b.Fatalf("create clip client: %v", err)
	}
	b.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func BenchmarkClientEmbedTextIntegration(b *testing.B) {
	client := requireClipBenchClient(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.EmbedText(ctx, "golden retriever on a beach"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClientEmbedImageBytesIntegration(b *testing.B) {
	client := requireClipBenchClient(b)
	ctx := context.Background()

	b.SetBytes(int64(len(integrationPNGBytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.EmbedImageBytes(ctx, integrationPNGBytes); err != nil {
			b.Fatal(err)
		}
	}
}
