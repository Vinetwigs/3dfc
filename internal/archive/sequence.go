// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func naturalLess(a, b string) bool {
	x, y := []rune(a), []rune(b)
	for i, j := 0, 0; i < len(x) && j < len(y); {
		if isDigit(x[i]) && isDigit(y[j]) {
			i0, j0 := i, j
			for i < len(x) && isDigit(x[i]) {
				i++
			}
			for j < len(y) && isDigit(y[j]) {
				j++
			}
			left, right := x[i0:i], y[j0:j]
			li, rj := 0, 0
			for li < len(left)-1 && left[li] == '0' {
				li++
			}
			for rj < len(right)-1 && right[rj] == '0' {
				rj++
			}
			ln, rn := len(left)-li, len(right)-rj
			if ln != rn {
				return ln < rn
			}
			for k := 0; k < ln; k++ {
				if left[li+k] != right[rj+k] {
					return left[li+k] < right[rj+k]
				}
			}
			if len(left) != len(right) {
				return len(left) < len(right)
			}
			continue
		}
		lx, ry := unicode.ToLower(x[i]), unicode.ToLower(y[j])
		if lx != ry {
			return lx < ry
		}
		if x[i] != y[j] {
			return x[i] < y[j]
		}
		i++
		j++
	}
	if len(x) != len(y) {
		return len(x) < len(y)
	}
	return a < b
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func sequenceWarnings(files []FrameFile) []string {
	type group struct {
		prefix string
		nums   []int
	}
	groups := map[string]*group{}
	for _, f := range files {
		stem := strings.TrimSuffix(f.Name, filepath.Ext(f.Name))
		i := len(stem)
		for i > 0 && stem[i-1] >= '0' && stem[i-1] <= '9' {
			i--
		}
		if i == len(stem) {
			continue
		}
		n, err := strconv.Atoi(stem[i:])
		if err != nil {
			continue
		}
		prefix := stem[:i]
		displayPrefix := prefix
		if displayPrefix == "" {
			displayPrefix = "numeric"
		}
		key := strings.ToLower(prefix + filepath.Ext(f.Name))
		g := groups[key]
		if g == nil {
			g = &group{prefix: displayPrefix}
			groups[key] = g
		}
		g.nums = append(g.nums, n)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var warnings []string
	for _, k := range keys {
		g := groups[k]
		if len(g.nums) < 2 {
			continue
		}
		sort.Ints(g.nums)
		var missing int64
		duplicates := 0
		for i := 1; i < len(g.nums); i++ {
			gap := int64(g.nums[i]) - int64(g.nums[i-1]) - 1
			if gap > 0 {
				missing += gap
			} else if gap == -1 {
				duplicates++
			}
		}
		if missing > 0 {
			warnings = append(warnings, fmt.Sprintf("possible missing frames in %q sequence: %d number(s) skipped between %d and %d", g.prefix, missing, g.nums[0], g.nums[len(g.nums)-1]))
		}
		if duplicates > 0 {
			warnings = append(warnings, fmt.Sprintf("duplicate frame numbers in %q sequence: %d duplicate(s)", g.prefix, duplicates))
		}
	}
	return warnings
}
