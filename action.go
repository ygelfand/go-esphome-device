package esphomedevice

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// An Action is something Home Assistant can call on the device, registered as esphome.<node>_<name>.
// Unlike an entity it holds no state: it takes arguments, does something, and may answer.
//
// Answering is how a device serves what a state cannot carry — bytes, or a list. Home Assistant decodes
// the answer as JSON and gives it to whoever called with return_response, so Answers must marshal to a
// JSON object.
type Action struct {
	Name string
	Args []Arg

	// Run does the work. What it returns becomes the answer; nil means there is none. An error reaches
	// the caller as a failed action with its message.
	Run func(Call) (any, error)

	// Answers says the action has something to say. Home Assistant refuses return_response on one that
	// does not, and refuses to wait for one that says it only ever answers.
	Answers bool
}

// ArgType is what an argument carries. Home Assistant checks a call against these before it arrives.
type ArgType int

const (
	ArgString ArgType = iota
	ArgInt
	ArgFloat
	ArgBool
)

type Arg struct {
	Name string
	Type ArgType
}

// Call is one invocation's arguments, by name.
type Call struct {
	args map[string]*api.ExecuteServiceArgument
}

func (c Call) String(name string) string {
	if a := c.args[name]; a != nil {
		return a.GetString_()
	}
	return ""
}

// Int reads an integer argument. Home Assistant sends an int in one of two fields depending on how old
// the client is, so both are read and whichever is set wins.
func (c Call) Int(name string) int {
	a := c.args[name]
	if a == nil {
		return 0
	}
	if v := a.GetInt_(); v != 0 {
		return int(v)
	}
	return int(a.GetLegacyInt())
}

func (c Call) Float(name string) float32 {
	if a := c.args[name]; a != nil {
		return a.GetFloat_()
	}
	return 0
}

func (c Call) Bool(name string) bool {
	if a := c.args[name]; a != nil {
		return a.GetBool_()
	}
	return false
}

func (a *Action) key() uint32 { return fnv1("action:" + a.Name) }

func (a *Action) describe() proto.Message {
	args := make([]*api.ListEntitiesServicesArgument, 0, len(a.Args))
	for _, arg := range a.Args {
		args = append(args, &api.ListEntitiesServicesArgument{Name: arg.Name, Type: wireType(arg.Type)})
	}

	answers := api.SupportsResponseType_SUPPORTS_RESPONSE_NONE
	if a.Answers {
		answers = api.SupportsResponseType_SUPPORTS_RESPONSE_OPTIONAL
	}

	return &api.ListEntitiesServicesResponse{
		Name:             a.Name,
		Key:              a.key(),
		Args:             args,
		SupportsResponse: answers,
	}
}

func wireType(t ArgType) api.ServiceArgType {
	switch t {
	case ArgInt:
		return api.ServiceArgType_SERVICE_ARG_TYPE_INT
	case ArgFloat:
		return api.ServiceArgType_SERVICE_ARG_TYPE_FLOAT
	case ArgBool:
		return api.ServiceArgType_SERVICE_ARG_TYPE_BOOL
	default:
		return api.ServiceArgType_SERVICE_ARG_TYPE_STRING
	}
}

// call runs the action and builds the reply. A caller that did not ask for one gets nothing back, which
// is what makes an action that only does something as cheap as it looks.
func (a *Action) call(m *api.ExecuteServiceRequest) proto.Message {
	named := make(map[string]*api.ExecuteServiceArgument, len(m.GetArgs()))
	for i, arg := range m.GetArgs() {
		if i < len(a.Args) {
			named[a.Args[i].Name] = arg
		}
	}

	answer, err := a.Run(Call{args: named})
	if !m.GetReturnResponse() {
		return nil
	}

	reply := &api.ExecuteServiceResponse{CallId: m.GetCallId(), Success: err == nil}
	if err != nil {
		reply.ErrorMessage = err.Error()
		return reply
	}
	if answer == nil {
		return reply
	}

	data, err := json.Marshal(answer)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("encoding the answer failed: %v", err)
		return reply
	}
	reply.ResponseData = data
	return reply
}
