// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

var nilErrorValue = reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())

// ----------------------------------------------------------------------------
// Codec
// ----------------------------------------------------------------------------

// Codec creates a CodecRequest to process each request.
type Codec interface {
	NewRequest(*http.Request) CodecRequest
}

// CodecRequest decodes a request and encodes a response using a specific
// serialization scheme.
type CodecRequest interface {
	// Reads the request and returns the RPC method name.
	Method() (string, error)
	// Reads the request filling the RPC method args.
	ReadRequest(interface{}) error
	// Writes the response using the RPC method reply.
	WriteResponse(http.ResponseWriter, interface{})
	// Writes an error produced by the server.
	WriteError(w http.ResponseWriter, status int, err error)
}

// ----------------------------------------------------------------------------
// Server
// ----------------------------------------------------------------------------

// NewServer returns a new RPC server.
func NewServer() *Server {
	return &Server{
		codecs:         make(map[string]Codec),
		services:       new(serviceMap),
		allowedMethods: []string{"POST", "GET", "DELETE", "PUT"},
	}
}

// RequestInfo contains all the information we pass to before/after functions
type RequestInfo struct {
	Method     string
	Error      error
	Request    *http.Request
	StatusCode int
}

// Server serves registered RPC services using registered codecs.
type Server struct {
	codecs         map[string]Codec
	services       *serviceMap
	interceptFunc  func(i *RequestInfo) *http.Request
	beforeFunc     func(i *RequestInfo)
	afterFunc      func(i *RequestInfo)
	validateFunc   reflect.Value
	allowedMethods []string
}

// RegisterCodec adds a new codec to the server.
//
// Codecs are defined to process a given serialization scheme, e.g., JSON or
// XML. A codec is chosen based on the "Content-Type" header from the request,
// excluding the charset definition.
func (s *Server) RegisterCodec(codec Codec, contentType string) {
	s.codecs[strings.ToLower(contentType)] = codec
}

// RegisterInterceptFunc registers the specified function as the function
// that will be called before every request. The function is allowed to intercept
// the request e.g. add values to the context.
//
// Note: Only one function can be registered, subsequent calls to this
// method will overwrite all the previous functions.
func (s *Server) RegisterInterceptFunc(f func(i *RequestInfo) *http.Request) {
	s.interceptFunc = f
}

// RegisterBeforeFunc registers the specified function as the function
// that will be called before every request.
//
// Note: Only one function can be registered, subsequent calls to this
// method will overwrite all the previous functions.
func (s *Server) RegisterBeforeFunc(f func(i *RequestInfo)) {
	s.beforeFunc = f
}

// RegisterValidateRequestFunc registers the specified function as the function
// that will be called after the BeforeFunc (if registered) and before invoking
// the actual Service method. If this function returns a non-nil error, the method
// won't be invoked and this error will be considered as the method result.
// The first argument is information about the request, useful for accessing to http.Request.Context()
// The second argument of this function is the already-unmarshalled *args parameter of the method.
func (s *Server) RegisterValidateRequestFunc(f func(r *RequestInfo, i interface{}) error) {
	s.validateFunc = reflect.ValueOf(f)
}

// RegisterAfterFunc registers the specified function as the function
// that will be called after every request
//
// Note: Only one function can be registered, subsequent calls to this
// method will overwrite all the previous functions.
func (s *Server) RegisterAfterFunc(f func(i *RequestInfo)) {
	s.afterFunc = f
}

// EnableGET enables GET HTTP method for RPC calls
func (s *Server) DisableGET() {
	s.removeMethod("GET")
}

func (s *Server) DisablePOST() {
	s.removeMethod("POST")
}

func (s *Server) DisablePUT() {
	s.removeMethod("PUT")
}

func (s *Server) DisableDelete() {
	s.removeMethod("DELETE")
}

func (s *Server) removeMethod(methodToRemove string) {
	var newMethods []string

	for _, method := range s.allowedMethods {
		if method != methodToRemove {
			newMethods = append(newMethods, method)
		}
	}
	s.allowedMethods = newMethods
}

// RegisterService adds a new service to the server.
//
// The name parameter is optional: if empty it will be inferred from
// the receiver type name.
//
// Methods from the receiver will be extracted if these rules are satisfied:
//
//   - The receiver is exported (begins with an upper case letter) or local
//     (defined in the package registering the service).
//   - The method name is exported.
//   - The method has three arguments: *http.Request, *args, *reply.
//   - All three arguments are pointers.
//   - The second and third arguments are exported or local.
//   - The method has return type error.
//
// All other methods are ignored.
func (s *Server) RegisterService(receiver interface{}, name string) error {
	return s.services.register(receiver, name)
}

// HasMethod returns true if the given method is registered.
//
// The method uses a dotted notation as in "Service.Method".
func (s *Server) HasMethod(method string) bool {
	if _, _, err := s.services.get(method); err == nil {
		return true
	}

	// If not found with exact case, try with proper capitalization
	methodCapitalized := capitalizeMethod(method)
	if methodCapitalized != method {
		if _, _, err := s.services.get(methodCapitalized); err == nil {
			return true
		}
	}

	return false
}

// capitalizeMethod converts a method name to proper case (e.g., "service.method" to "Service.Method")
func capitalizeMethod(method string) string {
	parts := strings.Split(method, ".")
	if len(parts) != 2 {
		return method // Not in expected format
	}

	// Capitalize first letter of service and method
	service := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	methodName := strings.ToUpper(parts[1][:1]) + parts[1][1:]

	return service + "." + methodName
}

// getNormalizedMethod gets the properly capitalized method name if it exists
func (s *Server) getNormalizedMethod(method string) (string, error) {
	// Try exact match first
	if _, _, err := s.services.get(method); err == nil {
		return method, nil
	}

	// If not found, try with proper capitalization
	methodCapitalized := capitalizeMethod(method)
	if methodCapitalized != method {
		if _, _, err := s.services.get(methodCapitalized); err == nil {
			return methodCapitalized, nil
		}
	}

	return "", fmt.Errorf("rpc: method not found: %s", method)
}

// ServeHTTP
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if the HTTP method is allowed
	methodAllowed := false
	for _, m := range s.allowedMethods {
		if r.Method == m {
			methodAllowed = true
			break
		}
	}

	if !methodAllowed {
		WriteError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("rpc: only %s methods are allowed, received %s",
				strings.Join(s.allowedMethods, ","), r.Method))
		return
	}

	// Extract method from URL path for GET requests with format /rpc/<method>
	var methodFromPath string
	if strings.HasPrefix(r.URL.Path, "/rpc/") {
		methodFromPath = strings.TrimPrefix(r.URL.Path, "/rpc/")
		if methodFromPath != "" {
			// Normalize method name if needed and verify it exists
			normalizedMethod, err := s.getNormalizedMethod(methodFromPath)
			if err != nil {
				WriteError(w, http.StatusNotFound, err.Error())
				return
			}
			methodFromPath = normalizedMethod
		}
	}

	// For GET requests, we don't enforce the Content-Type header
	var codec Codec
	if r.Method == "GET" {
		// If only one codec has been registered, use that
		if len(s.codecs) == 1 {
			for _, c := range s.codecs {
				codec = c
			}
		} else {
			// Try to find a codec that supports GET, preferably application/json
			if jsonCodec, ok := s.codecs["application/json"]; ok {
				codec = jsonCodec
			} else {
				// Pick the first codec
				for _, c := range s.codecs {
					codec = c
					break
				}
			}
		}
	} else {
		// For non-GET requests, use Content-Type based selection
		contentType := r.Header.Get("Content-Type")
		idx := strings.Index(contentType, ";")
		if idx != -1 {
			contentType = contentType[:idx]
		}
		if contentType == "" && len(s.codecs) == 1 {
			// If Content-Type is not set and only one codec has been registered,
			// then default to that codec.
			for _, c := range s.codecs {
				codec = c
			}
		} else if codec = s.codecs[strings.ToLower(contentType)]; codec == nil {
			WriteError(w, http.StatusUnsupportedMediaType, "rpc: unrecognized Content-Type: "+contentType)
			return
		}
	}

	// Create a new codec request.
	codecReq := codec.NewRequest(r)

	// Get service method to be called.
	var method string
	var errMethod error

	if methodFromPath != "" {
		// Use method from URL path for GET requests
		method = methodFromPath
	} else {
		// Otherwise get it from the codec request
		method, errMethod = codecReq.Method()
		if errMethod != nil {
			codecReq.WriteError(w, http.StatusBadRequest, errMethod)
			return
		}

		// Handle lowercase methods that weren't extracted from path
		normalizedMethod, err := s.getNormalizedMethod(method)
		if err == nil {
			method = normalizedMethod
		} else {
			codecReq.WriteError(w, http.StatusBadRequest, err)
			return
		}
	}

	serviceSpec, methodSpec, errGet := s.services.get(method)
	if errGet != nil {
		codecReq.WriteError(w, http.StatusBadRequest, errGet)
		return
	}

	// Handle args differently based on whether the method takes them
	var args reflect.Value
	if !methodSpec.noArgs {
		// Regular method with args parameter
		args = reflect.New(methodSpec.argsType)
		// Make arguments optional - don't return an error if we can't read them
		errRead := codecReq.ReadRequest(args.Interface())
		if errRead != nil {
			// Check if args type is an empty struct (no exported fields)
			hasExportedFields := false
			argsType := methodSpec.argsType
			for i := 0; i < argsType.NumField(); i++ {
				field := argsType.Field(i)
				// Check if field is exported (starts with uppercase)
				if field.PkgPath == "" {
					hasExportedFields = true
					break
				}
			}

			// If args have exported fields, return the error
			if hasExportedFields {
				codecReq.WriteError(w, http.StatusBadRequest, errRead)
				return
			}
		}
	} else {
		// NoArgs method - no args parameter needed
		// Create a dummy empty struct just for internal usage
		args = reflect.New(reflect.TypeOf(struct{}{}))
	}

	// Call the registered Intercept Function
	if s.interceptFunc != nil {
		req := s.interceptFunc(&RequestInfo{
			Request: r,
			Method:  method,
		})
		if req != nil {
			r = req
		}
	}

	requestInfo := &RequestInfo{
		Request: r,
		Method:  method,
	}

	// Call the registered Before Function
	if s.beforeFunc != nil {
		s.beforeFunc(requestInfo)
	}

	// Prepare the reply, we need it even if validation fails
	reply := reflect.New(methodSpec.replyType)
	errValue := []reflect.Value{nilErrorValue}

	// Call the registered Validator Function if this is a method with args
	if s.validateFunc.IsValid() && !methodSpec.noArgs {
		errValue = s.validateFunc.Call([]reflect.Value{reflect.ValueOf(requestInfo), args})
	}

	// If still no errors after validation, call the method
	if errValue[0].IsNil() {
		var callArgs []reflect.Value

		if methodSpec.noArgs {
			// For NoArgs methods, only pass receiver, request, and reply
			callArgs = []reflect.Value{
				serviceSpec.rcvr,
				reflect.ValueOf(r),
				reply,
			}
		} else {
			// For regular methods, pass receiver, request, args, and reply
			callArgs = []reflect.Value{
				serviceSpec.rcvr,
				reflect.ValueOf(r),
				args,
				reply,
			}
		}

		errValue = methodSpec.method.Func.Call(callArgs)
	}

	// Extract the result to error if needed.
	var errResult error
	statusCode := http.StatusOK
	errInter := errValue[0].Interface()
	if errInter != nil {
		statusCode = http.StatusBadRequest
		errResult = errInter.(error)
	}

	// Prevents Internet Explorer from MIME-sniffing a response away
	// from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")

	// Encode the response.
	if errResult == nil {
		codecReq.WriteResponse(w, reply.Interface())
	} else {
		codecReq.WriteError(w, statusCode, errResult)
	}

	// Call the registered After Function
	if s.afterFunc != nil {
		s.afterFunc(&RequestInfo{
			Request:    r,
			Method:     method,
			Error:      errResult,
			StatusCode: statusCode,
		})
	}
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, msg)
}
