// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This would be part of the service_map.go file in your RPC package

package rpc

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// serviceMap is a registry for services.
type serviceMap struct {
	services map[string]*service
}

// service represents a registered service.
type service struct {
	name     string                    // name of service
	rcvr     reflect.Value             // receiver of methods for the service
	rcvrType reflect.Type              // type of the receiver
	methods  map[string]*serviceMethod // registered methods
}

// serviceMethod represents a registered method of a service.
type serviceMethod struct {
	method    reflect.Method // receiver method
	argsType  reflect.Type   // type of the request argument
	replyType reflect.Type   // type of the response argument
	noArgs    bool           // true if method doesn't have args parameter
}

// register adds a new service using reflection to extract its methods.
func (m *serviceMap) register(rcvr interface{}, name string) error {
	// Setup service.
	s := &service{
		rcvr:     reflect.ValueOf(rcvr),
		rcvrType: reflect.TypeOf(rcvr),
		methods:  make(map[string]*serviceMethod),
	}
	if name != "" {
		s.name = name
	} else {
		s.name = reflect.Indirect(s.rcvr).Type().Name()
		if !isExported(s.name) {
			return fmt.Errorf("rpc: type %q is not exported", s.name)
		}
	}
	if m.services == nil {
		m.services = make(map[string]*service)
	} else if _, ok := m.services[s.name]; ok {
		return fmt.Errorf("rpc: service already defined: %q", s.name)
	}

	// Setup methods.
	for i := 0; i < s.rcvrType.NumMethod(); i++ {
		method := s.rcvrType.Method(i)
		if !isExported(method.Name) {
			continue
		}

		// Method must be exported.
		if method.PkgPath != "" {
			continue
		}

		// Method needs right number of ins: receiver, *http.Request, *args, *reply or
		// receiver, *http.Request, *reply (for NoArgs)
		var noArgs bool
		if method.Type.NumIn() == 4 {
			// Regular method with args
			noArgs = false
		} else if method.Type.NumIn() == 3 {
			// Method without args
			noArgs = true
		} else {
			continue
		}

		// First argument must be a pointer and must be http.Request.
		httpReqType := reflect.TypeOf(&http.Request{})
		if method.Type.In(1) != httpReqType {
			continue
		}

		var argType, replyType reflect.Type

		if !noArgs {
			// Second argument must be a pointer.
			argType = method.Type.In(2)
			if argType.Kind() != reflect.Ptr {
				continue
			}
			// Second argument must be exported.
			if !isExportedOrBuiltin(argType) {
				continue
			}

			// Third argument must be a pointer.
			replyType = method.Type.In(3)
			if replyType.Kind() != reflect.Ptr {
				continue
			}
			// Third argument must be exported.
			if !isExportedOrBuiltin(replyType) {
				continue
			}
		} else {
			// For NoArgs methods: Second argument must be a pointer (reply)
			replyType = method.Type.In(2)
			if replyType.Kind() != reflect.Ptr {
				continue
			}
			// Second argument must be exported.
			if !isExportedOrBuiltin(replyType) {
				continue
			}

			// Create a dummy argType for NoArgs methods
			argType = reflect.TypeOf(&struct{}{})
		}

		// Method needs one out: error.
		if method.Type.NumOut() != 1 {
			continue
		}
		if method.Type.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		s.methods[method.Name] = &serviceMethod{
			method:    method,
			argsType:  argType.Elem(),
			replyType: replyType.Elem(),
			noArgs:    noArgs,
		}
	}

	if len(s.methods) == 0 {
		return fmt.Errorf("rpc: %q has no exported methods of suitable type", s.name)
	}
	m.services[s.name] = s
	return nil
}

// get returns a registered service given a method name.
//
// The method name uses a dotted notation as in "Service.Method".
func (m *serviceMap) get(method string) (*service, *serviceMethod, error) {
	parts := strings.Split(method, ".")
	if len(parts) != 2 {
		return nil, nil, errors.New("rpc: service/method request ill-formed: " + method)
	}
	parts[0] = strings.Title(parts[0])
	parts[1] = strings.Title(parts[1])
	service := m.services[parts[0]]
	if service == nil {
		return nil, nil, errors.New("rpc: can't find service " + parts[0])
	}
	serviceMethod := service.methods[parts[1]]
	if serviceMethod == nil {
		return nil, nil, errors.New("rpc: can't find method " + parts[1])
	}
	return service, serviceMethod, nil
}

// isExported returns true if the name is exported (starts with an upper case
// letter).
func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

// isExportedOrBuiltin returns true if a type is exported or a builtin.
func isExportedOrBuiltin(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}
