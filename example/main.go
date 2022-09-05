package main


import (
	"net/http"
	"github.com/shridarpatil/rpc"
	"github.com/shridarpatil/rpc/json"
)


type HelloService struct{}


type HelloReply struct {
	Message string
}


type HelloArgs struct {
	Who string
}


func (h *HelloService) Say(r *http.Request, args *HelloArgs, reply *HelloReply) error {


	reply.Message = "Hello, " + args.Who + "!"
	return nil
}



func main(){
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterService(&HelloService{}, "")

	http.Handle("/rpc", server)
	http.ListenAndServe("localhost:10000", nil);
}