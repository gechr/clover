package directive

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/x/human"
)

// KV is one key/value pair from a directive, in the order written.
type KV struct {
	Key   string
	Value string
}

// Directive is a parsed clover: directive: its key/value pairs in source order.
// Order is preserved (a slice, not a map) because keys repeat - include and
// exclude may appear several times - and because format mode reorders the
// pairs into a canonical sequence, which it can only do from the original.
// Parsing is purely syntactic: keys are not validated and values are not
// interpreted here; that is the provider's and the rule's job downstream.
type Directive struct {
	Pairs []KV
}

// Get returns the value of the first pair with the given key.
func (d Directive) Get(key string) (string, bool) {
	for _, kv := range d.Pairs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

// All returns every value for key, in order - for repeatable keys like include
// and exclude.
func (d Directive) All(key string) []string {
	var values []string
	for _, kv := range d.Pairs {
		if kv.Key == key {
			values = append(values, kv.Value)
		}
	}
	return values
}

// Has reports whether key is present at all.
func (d Directive) Has(key string) bool {
	_, ok := d.Get(key)
	return ok
}

// Bool interprets the first value for key as a boolean. An absent key is false
// with no error; a present key must be exactly true or false, so a typo like
// skip=yes is rejected rather than silently treated as false.
func (d Directive) Bool(key string) (bool, error) {
	v, ok := d.Get(key)
	if !ok {
		return false, nil
	}
	switch v {
	case constant.BoolTrue:
		return true, nil
	case constant.BoolFalse:
		return false, nil
	default:
		return false, fmt.Errorf(
			"%s must be %s or %s, got %q",
			key,
			constant.BoolTrue,
			constant.BoolFalse,
			v,
		)
	}
}

// Int interprets the first value for key as an integer. An absent key is zero
// with no error; a present non-integer value is rejected. Range checks (e.g. a
// key that must be non-negative) belong to the validating caller.
func (d Directive) Int(key string) (int, error) {
	v, ok := d.Get(key)
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, v)
	}
	return n, nil
}

// Duration interprets the first value for key as a human duration (e.g. 2w3d,
// 72h, 30m). An absent key is zero with no error; a present unparseable value is
// rejected. Day, week, and year units are supported beyond the standard
// library's hours-and-below.
func (d Directive) Duration(key string) (time.Duration, error) {
	v, ok := d.Get(key)
	if !ok {
		return 0, nil
	}
	dur, err := human.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration like 2w3d, got %q", key, v)
	}
	return dur, nil
}
