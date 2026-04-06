// Copyright 2026 Adrien Ndikumana
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

package parsers

import (
	"fmt"
	"sync"

	"github.com/adrien19/noc-foundry/internal/network/models"
)

// ParserKey identifies a registered CLI parser by vendor, platform,
// operation, and output format.
type ParserKey struct {
	Vendor    string
	Platform  string
	Operation string
	// Format is the output encoding the parser handles: "text", "json", "xml".
	Format string
}

// CLIParserFunc converts raw CLI output into a canonical payload and
// quality metadata. It returns an error only for unrecoverable failures
// (e.g. malformed JSON on a declared-JSON path).
type CLIParserFunc func(raw string) (any, models.QualityMeta, error)

var (
	parserMu       sync.RWMutex
	parserRegistry = map[ParserKey]CLIParserFunc{}
)

// RegisterParser adds a CLI parser for the given key.
// Panics if a parser is already registered for the same key; this
// guards against accidental double-registration in init() chains.
func RegisterParser(key ParserKey, fn CLIParserFunc) {
	parserMu.Lock()
	defer parserMu.Unlock()
	if _, exists := parserRegistry[key]; exists {
		panic(fmt.Sprintf("CLI parser already registered for key %+v", key))
	}
	parserRegistry[key] = fn
}

// Dispatch routes raw CLI output to the registered parser for key.
// If no parser is found the raw string is returned as-is with a
// MappingPartial warning; the error return is always nil in that case.
func Dispatch(key ParserKey, raw string) (any, models.QualityMeta, error) {
	parserMu.RLock()
	fn, ok := parserRegistry[key]
	parserMu.RUnlock()
	if !ok {
		return raw, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings: []string{fmt.Sprintf(
				"no CLI parser registered for vendor=%s platform=%s op=%s format=%s",
				key.Vendor, key.Platform, key.Operation, key.Format,
			)},
		}, nil
	}
	return fn(raw)
}
