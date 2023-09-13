// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (s *Server) periodicBackup(ctx context.Context) {
	lastWriteGen := uint64(0)
	for {
		gen := s.db.WriteGen()
		if gen != lastWriteGen {
			if err := s.doBackup(ctx); err != nil {
				log.Printf("Failed to take backup: %v", err)
			} else {
				lastWriteGen = gen
			}
			select {
			case <-time.After(time.Minute):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Server) doBackup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	start := time.Now()

	path := s.db.Path()
	bs, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	expectSum := fmt.Sprintf("\"%x\"", md5.Sum(bs))

	key := backupKey()

	resp, err := s.backupClient.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.backupBucket,
		Key:    &key,
		Body:   bytes.NewReader(bs),
	})
	if err != nil {
		return err
	}
	if *resp.ETag != expectSum {
		return fmt.Errorf("md5 mismatch: expected %s, got %s", expectSum, *resp.ETag)
	}

	name := filepath.Base(path)
	log.Printf("Uploaded file %q to %s/%s with ETag: %v. Took %v", name, s.backupBucket, key, *resp.ETag, time.Since(start).Round(time.Millisecond))
	return nil
}

func backupKey() string {
	now := time.Now().Round(time.Second)
	return fmt.Sprintf("%d/%d/%d/db-%s.json", now.Year(), now.Month(), now.Day(), now.Format(time.RFC3339))
}
