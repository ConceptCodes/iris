package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"iris/internal/jobs"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

func enqueueURLFile(ctx context.Context, jobStore jobs.Store, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open url file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		payload, err := json.Marshal(jobs.FetchImagePayload{URL: line})
		if err != nil {
			return err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.TypeFetchImage,
			PayloadJSON: payload,
		}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func enqueueDir(ctx context.Context, jobStore jobs.Store, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !imageExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}

		payload, err := json.Marshal(jobs.IndexLocalFilePayload{Path: path})
		if err != nil {
			return err
		}
		_, err = jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.TypeIndexLocalFile,
			PayloadJSON: payload,
		})
		return err
	})
}
