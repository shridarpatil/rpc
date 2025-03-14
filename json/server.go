// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/shridarpatil/rpc"
)

var null = json.RawMessage([]byte("null"))

// An Error is a wrapper for a JSON interface value. It can be used by either
// a service's handler func to write more complex JSON data to an error field
// of a server's response, or by a client to read it.
type Error struct {
	Data interface{}
}

func (e *Error) Error() string {
	return fmt.Sprintf("%v", e.Data)
}

// ----------------------------------------------------------------------------
// Request and Response
// ----------------------------------------------------------------------------

// serverRequest represents a JSON-RPC request received by the server.
type serverRequest struct {
	// A String containing the name of the method to be invoked.
	Method string `json:"method"`
	// An Array of objects to pass as arguments to the method.
	Params *json.RawMessage `json:"params"`
	// The request id. This can be of any type. It is used to match the
	// response with the request that it is replying to.
	// Id *json.RawMessage `json:"id"`
}

// serverResponse represents a JSON-RPC response returned by the server.
type serverResponse struct {
	// The Object that was returned by the invoked method. This must be null
	// in case there was an error invoking the method.
	Result interface{} `json:"result"`
	// An Error object if there was an error invoking the method. It must be
	// null if there was no error.
	Error interface{} `json:"error"`
	// This must be the same id as the request it is responding to.
	// Id *json.RawMessage `json:"id"`
}

// ----------------------------------------------------------------------------
// Codec
// ----------------------------------------------------------------------------

// NewCodec returns a new JSON Codec.
func NewCodec() *Codec {
	return &Codec{}
}

// Codec creates a CodecRequest to process each request.
type Codec struct {
}

// NewRequest returns a CodecRequest.
func (c *Codec) NewRequest(r *http.Request) rpc.CodecRequest {
	// Parse URL parameters for all requests to extract method if present
	err := r.ParseForm()
	if err != nil {
		return &CodecRequest{request: nil, err: err}
	}

	// Check for method in URL path or query parameters
	methodFromURL := extractMethodFromURL(r)

	if r.Method == "GET" {
		return newGetCodecRequest(r, methodFromURL)
	}

	return newPostCodecRequest(r, methodFromURL)
}

// extractMethodFromURL extracts method name from either URL path or query parameters
func extractMethodFromURL(r *http.Request) string {
	var methodName string

	// First check if method is in path like /rpc/<method>
	if strings.HasPrefix(r.URL.Path, "/rpc/") {
		methodPath := strings.TrimPrefix(r.URL.Path, "/rpc/")
		if methodPath != "" {
			methodName = methodPath
		}
	}

	// If not found, check last part of any path
	if methodName == "" {
		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(pathParts) > 0 && pathParts[len(pathParts)-1] != "" {
			// Use the last part of the path as the method name
			methodName = pathParts[len(pathParts)-1]
		}
	}

	// If still not found, check query parameter
	if methodName == "" {
		if methodParam := r.Form.Get("method"); methodParam != "" {
			methodName = methodParam
		}
	}

	// Format the method name properly - first character of service and method should be uppercase
	if methodName != "" {
		parts := strings.Split(methodName, ".")
		if len(parts) == 2 {
			// Properly capitalize the service name and method name
			service := strings.ToUpper(parts[0][:1]) + parts[0][1:]
			method := strings.ToUpper(parts[1][:1]) + parts[1][1:]
			methodName = service + "." + method
		}
	}

	return methodName
}

// ----------------------------------------------------------------------------
// CodecRequest
// ----------------------------------------------------------------------------

// newPostCodecRequest returns a new CodecRequest for POST requests.
func newPostCodecRequest(r *http.Request, methodFromURL string) rpc.CodecRequest {
	// Decode the request body
	req := new(serverRequest)
	err := json.NewDecoder(r.Body).Decode(req)
	r.Body.Close()

	// If method is specified in URL and not in the JSON body, use the URL method
	if err == nil && methodFromURL != "" && req.Method == "" {
		req.Method = methodFromURL
	}

	// If there's an error with the JSON body but we have a method from URL,
	// create a new request with just that method
	if err != nil && methodFromURL != "" {
		req = &serverRequest{
			Method: methodFromURL,
		}
		err = nil
	}

	return &CodecRequest{request: req, err: err}
}

// newGetCodecRequest returns a new CodecRequest for GET requests.
func newGetCodecRequest(r *http.Request, methodFromURL string) rpc.CodecRequest {
	if methodFromURL == "" {
		return &CodecRequest{request: nil, err: errors.New("rpc: method name missing")}
	}

	// Convert query parameters to JSON params
	paramsJSON, err := convertURLParamsToJSON(r.Form)
	if err != nil {
		return &CodecRequest{request: nil, err: err}
	}

	req := &serverRequest{
		Method: methodFromURL,
		Params: &paramsJSON,
	}

	return &CodecRequest{request: req, err: nil}
}

// convertURLParamsToJSON converts URL query parameters to a JSON-RPC params structure
func convertURLParamsToJSON(form url.Values) (json.RawMessage, error) {
	// Remove method parameter if it exists as it's already handled
	delete(form, "method")
	// Create a map from the query parameters
	paramsMap := make(map[string]interface{})
	for key, values := range form {
		if len(values) == 1 {
			paramsMap[key] = values[0]
		} else if len(values) > 1 {
			paramsMap[key] = values
		}
	}
	// Marshal the map directly - don't wrap in an array
	jsonData, err := json.Marshal(paramsMap)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(jsonData), nil
}

// CodecRequest decodes and encodes a single request.
type CodecRequest struct {
	request *serverRequest
	err     error
}

// Method returns the RPC method for the current request.
//
// The method uses a dotted notation as in "Service.Method".
func (c *CodecRequest) Method() (string, error) {
	if c.err == nil {
		return c.request.Method, nil
	}
	return "", c.err
}

// ReadRequest fills the request object for the RPC method.
func (c *CodecRequest) ReadRequest(args interface{}) error {
	if c.err == nil {
		if c.request.Params != nil {
			// Directly unmarshal params into the args struct
			c.err = json.Unmarshal(*c.request.Params, args)
		} else {
			// For POST requests with empty body but method in URL,
			// create empty params if needed
			emptyParams := []byte("{}")
			rawMessage := json.RawMessage(emptyParams)
			c.request.Params = &rawMessage
			c.err = json.Unmarshal(*c.request.Params, args)
		}
	}
	return c.err
}

// WriteResponse encodes the response and writes it to the ResponseWriter.
func (c *CodecRequest) WriteResponse(w http.ResponseWriter, reply interface{}) {
	// if c.request.Id != nil {
	// 	// Id is null for notifications and they don't have a response.
	// }
	res := &serverResponse{
		Result: reply,
		Error:  &null,
		// Id:     c.request.Id,
	}
	c.writeServerResponse(w, 200, res)
}

func (c *CodecRequest) WriteError(w http.ResponseWriter, _ int, err error) {
	res := &serverResponse{
		Result: &null,
		// Id:     c.request.Id,
	}
	if jsonErr, ok := err.(*Error); ok {
		res.Error = jsonErr.Data
	} else {
		res.Error = err.Error()
	}
	c.writeServerResponse(w, 400, res)
}

func (c *CodecRequest) writeServerResponse(w http.ResponseWriter, status int, res *serverResponse) {
	b, err := json.Marshal(res)
	if err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		w.Write(b)
	} else {
		// Not sure in which case will this happen. But seems harmless.
		rpc.WriteError(w, 400, err.Error())
	}
}
