// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"

	"golang.org/x/exp/slices"
)

// ParseFields parses information about setec-tagged fields from v. The
// concrete type of v must be a pointer to a struct value with at least one
// tagged field.  The namePrefix, if non-empty, is joined to the front of each
// tagged name, separated with a slash ("/").
//
// See [Fields] for a description of the struct tags and types recognized.
func ParseFields(v any, namePrefix string) (*Fields, error) {
	fi, err := parseFields(v)
	if err != nil {
		return nil, err
	}
	if len(fi) == 0 {
		return nil, fmt.Errorf("type %v has no setec-tagged fields", reflect.TypeOf(v).Elem())
	}
	return &Fields{prefix: namePrefix, fields: fi}, nil
}

// Fields is a helper for plumbing secrets to the fields of struct values.  The
// [ParseFields] function recognizes fields of a struct with a tag like:
//
//	setec:"base-secret-name[,json]"
//
// The resulting Fields value fetches the secrets identified by these tags from
// a setec.Store, and injects their values into the fields.
//
// # Background
//
// A program that uses multiple secret values has to plumb those secrets down
// to the code that needs them. One way to manage this is to bundle the secrets
// together into fields of a struct, and to make that struct accessible via a
// shared configuration library or through a context argument.
//
// To simplify the manual process of adding fields and hooking them up to the
// secrets service, the Fields type uses reflection to discover the names of
// secrets declared via struct tags, and to handle the boilerplate of plumbing
// secret values to those fields.
//
// # Basic usage
//
// Populate the Structs field of the [StoreConfig] with pointers to the struct
// values to be populated. You may optionally provide a prefix to prepend to
// secret names, so that it can use different secrets in different environments
// (for example dev vs. prod):
//
//	st, err := setec.NewStore(ctx, setec.StoreConfig{
//	   Client:  client,
//	   Structs: []setec.Struct{{Value: &v, Prefix: "dev/program-name"}},
//	})
//	// ...
//
// Once the store is ready, the secret values are automatically copied to the
// corresponding fields. It is also possible to explicitly populate struct
// fields after the store is constructed, see [ParseFields]. The store must be
// constructed with the AllowLookup option enabled to add new secrets after the
// store has been constructed.
//
// # Field Types
//
// The Fields type can handle struct fields of the following types:
//
//   - A field of type []byte receives a copy of the secret value.
//   - A field of type string receives a copy of the secret as a string.
//   - A field of type [setec.Secret] is populated with a handle to the secret.
//   - A field of type [setec.Watcher] is populated with a watcher for the secret.
//
// In addition, a field may have any type that supports JSON encoding, provided
// the secret value is also encoded as JSON, if its tag includes the optional
// "json" verb. For example, given:
//
//	type Key struct {
//	   Salt []byte `json:"iv"`
//	   Data []byte `json:"data"`
//	}
//
// the following is a valid field declaration:
//
//	SecretKey Key `setec:"secret-key,json"`  // note "json" verb
//
// and accepts a secret value formatted like:
//
//	{"iv":"aGVsbG8sIHdvcmxk","data":"c3VwZXIgc2VjcmV0IHNxdWlycmVsIHN0dWZm"}
//
// The ParseFields function will report an error for a tagged field whose type
// does not fit within these constraints.
type Fields struct {
	prefix string      // empty means "no prefix"
	fields []fieldInfo // fields needing populated
}

// Secrets returns the full prefix-expanded names of the secrets needed by
// fields tagged in f.
func (f *Fields) Secrets() []string {
	out := make([]string, len(f.fields))
	for i, fi := range f.fields {
		out[i] = path.Join(f.prefix, fi.secretName)
	}
	return out
}

// Apply fetches and applies the secret values required by f to the
// corresponding fields of the input struct. Each secret must either be known
// to s at initialization, or s must be configured to allow lookups.
// Apply will attempt to process all tagged fields before reporting an error.
//
// Note: When applying secrets to struct fields from an existing Store, the
// AllowLookup option of the Store must be enabled, or else Apply will report
// an error for any field that refers to a secret not already available.
func (f *Fields) Apply(ctx context.Context, s *Store) error {
	var errs []error
	for _, fi := range f.fields {
		fullName := path.Join(f.prefix, fi.secretName)
		if err := fi.apply(ctx, s, fullName); err != nil {
			errs = append(errs, fmt.Errorf("apply %q to field %q: %w", fullName, fi.fieldName, err))
		}
	}
	return errors.Join(errs...)
}

// fieldInfo records information about a tagged field.
type fieldInfo struct {
	fieldName  string        // name in the type (for diagnostics)
	secretName string        // name in the field tag (without prefix)
	value      reflect.Value // pointer to field
	isJSON     bool          // if true, secret must be JSON encoded
	vtype      reflect.Type  // type of field pointed to by value
}

// apply sets the target of fi.value to the secret named. It reports an error
// if the requested secret could not be fetched from the store.
//
// If f.isJSON is true, the data are unmarshaled as JSON.
// Otherwise, the data are converted to the target type and copied.
func (f fieldInfo) apply(ctx context.Context, s *Store, fullName string) error {
	if f.isJSON {
		v, err := s.LookupSecret(ctx, fullName)
		if err != nil {
			return err
		}
		return json.Unmarshal(v.Get(), f.value.Interface())
	}

	if f.vtype == watcherType {
		w, err := s.LookupWatcher(ctx, fullName)
		if err != nil {
			return err
		}
		f.value.Elem().Set(reflect.ValueOf(w))
		return nil
	}

	v, err := s.LookupSecret(ctx, fullName)
	if err != nil {
		return err
	}
	switch f.vtype {
	case bytesType:
		f.value.Elem().Set(reflect.ValueOf(v.Get()))
	case stringType:
		f.value.Elem().Set(reflect.ValueOf(string(v.Get())))
	case secretType:
		f.value.Elem().Set(reflect.ValueOf(v))
	default:
		return fmt.Errorf("unexpected field type %v", f.vtype)
	}
	return nil
}

var (
	bytesType   = reflect.TypeOf([]byte(nil))
	secretType  = reflect.TypeOf(Secret(nil))
	stringType  = reflect.TypeOf(string(""))
	watcherType = reflect.TypeOf(Watcher{})
)

// parseFields constructs a field list for obj, which must be a pointer to a
// struct. The result contains one entry for each field of *obj having a
// "setec" struct tag, giving the base name of the secret to use for that
// field.
func parseFields(obj any) ([]fieldInfo, error) {
	v := reflect.ValueOf(obj)
	vt := v.Type()
	if vt.Kind() != reflect.Pointer || vt.Elem().Kind() != reflect.Struct {
		return nil, errors.New("value is not a pointer to a struct")
	}
	vt = vt.Elem()
	var out []fieldInfo
	for _, ft := range reflect.VisibleFields(vt) {
		tag, ok := ft.Tag.Lookup("setec")
		if !ok {
			continue // not a relevant field
		}
		parts := strings.Split(tag, ",")
		if parts[0] == "" {
			return nil, fmt.Errorf("empty secret name for tagged field %q", ft.Name)
		}
		fi := fieldInfo{
			fieldName:  ft.Name,
			secretName: parts[0],
			value:      v.Elem().FieldByIndex(ft.Index).Addr(),
			isJSON:     slices.Contains(parts[1:], "json"),
			vtype:      ft.Type,
		}
		if !fi.isJSON {
			switch ft.Type {
			case bytesType, stringType, secretType, watcherType:
				// OK, these are supported
			default:
				return nil, fmt.Errorf("unsupported type %v for tagged field %q", ft.Type, ft.Name)
			}
		}
		out = append(out, fi)
	}
	return out, nil
}
