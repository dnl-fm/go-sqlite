package main

import (
	"errors"
	"fmt"

	"github.com/ebitengine/purego"
)

// malloc_trim returns whether glibc released memory, not whether all unused
// memory was released. Keep the result beside the RSS measurement.
func (r *recorder) trimSample(phase string, rows int64, pass, conns int, detail string) (m measurement, trimErr error) {
	handle, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return m, fmt.Errorf("load libc for malloc_trim: %w", err)
	}
	defer func() { trimErr = errors.Join(trimErr, purego.Dlclose(handle)) }()
	symbol, err := purego.Dlsym(handle, "malloc_trim")
	if err != nil {
		return m, fmt.Errorf("resolve malloc_trim: %w", err)
	}
	var trim func(uintptr) int32
	purego.RegisterFunc(&trim, symbol)
	result := trim(0)
	if detail != "" {
		detail += " "
	}
	return r.sample(phase, rows, pass, conns, detail+fmt.Sprintf("malloc_trim_result=%d", result))
}
