// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// substrateModule is the module whose public packages this one may import.
const substrateModule = "github.com/agent-substrate/substrate"

// A plugin imports only substrate's public packages, so a change to upstream's
// internal packages never breaks it and the proto contract is the whole
// dependency.
func TestImportsOnlySubstratePublicPackages(t *testing.T) {
	root := filepath.Join("..", "..")
	self := modulePath(t, filepath.Join(root, "go.mod"))

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// The go command ignores these, and so does the rule.
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			switch {
			case imp == self || strings.HasPrefix(imp, self+"/"):
			case strings.HasPrefix(imp, substrateModule+"/pkg/"):
			case imp == substrateModule || strings.HasPrefix(imp, substrateModule+"/"):
				t.Errorf("%s imports %s; this module may import only %s/pkg/...", path, imp, substrateModule)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
}

// modulePath reads the module path from a go.mod file.
func modulePath(t *testing.T, goMod string) string {
	t.Helper()
	f, err := os.Open(goMod) //nolint:gosec // the path is this module's own go.mod
	if err != nil {
		t.Fatalf("opening %s: %v", goMod, err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if path, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatalf("%s names no module", goMod)
	return ""
}
