package main

import (
	"fmt"
	"net/http"

	"github.com/shridarpatil/rpc"
	"github.com/shridarpatil/rpc/json"
)

// HelloService defines a simple RPC service
type HelloService struct{}

// HelloReply is the response structure
type HelloReply struct {
	Message string `json:"message"`
}

// HelloArgs defines the arguments for the Hello service
type HelloArgs struct {
	Who string `json:"who"`
}

// Say returns a greeting message
func (h *HelloService) Say(r *http.Request, args *HelloArgs, reply *HelloReply) error {
	reply.Message = "Hello, " + args.Who + "!"
	return nil
}

func (h *HelloService) NoArgs(r *http.Request, reply *HelloReply) error {
	reply.Message = "Hello!"
	return nil
}

func main() {
	// Create a new RPC server
	server := rpc.NewServer()
	fmt.Println(server)
	// Register JSON codec
	server.RegisterCodec(json.NewCodec(), "application/json")

	// Register our service
	server.RegisterService(&HelloService{}, "")

	server.DisableDelete()

	// Handle both the traditional endpoint and the path-based endpoint
	http.Handle("/rpc/", server) // Note the trailing slash for path-based routing

	// Also handle the original /rpc endpoint for backward compatibility
	http.Handle("/rpc", server)

	// Print some usage examples
	fmt.Println("Starting RPC server on localhost:10000")
	fmt.Println("Example GET request: http://localhost:10000/rpc/helloservice.say?who=World")
	fmt.Println("Example GET request: http://localhost:10000/rpc/helloservice.noargs?who=World")
	fmt.Println("Example POST request: curl -X POST -H \"Content-Type: application/json\" -d '{\"method\":\"helloservice.say\",\"params\":{\"who\":\"World\"}}' http://localhost:10000/rpc")

	// Start the server
	http.ListenAndServe("localhost:10000", nil)
}
