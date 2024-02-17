/* SPDX-License-Identifier: MIT
 *
  * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
  * Copyright (C) 2023 Synthesis Labs. All Rights Reserved.
 */
package main

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestFormatting(t *testing.T) {
	var cn sync.WaitGroup
	filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Errorf("unable to walk %s: %v", path, err)
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		cn.Add(1)
		go func(path string) {
			defer cn.Done()
			src, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("unable to read %s: %v", path, err)
				return
			}
			if runtime.GOOS == "windows" {
				src = bytes.ReplaceAll(src, []byte{'\r', '\n'}, []byte{'\n'})
			}
			formatted, err := format.Source(src)
			if err != nil {
				t.Errorf("unable to format %s: %v", path, err)
				return
			}
			if !bytes.Equal(src, formatted) {
				t.Errorf("unformatted code: %s", path)
			}
		}(path)
		return nil
	})
	cn.Wait()
}
