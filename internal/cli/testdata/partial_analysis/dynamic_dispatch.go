package partialanalysis

import (
	"fmt"
	"reflect"
)

// DynamicDispatcher dynamically dispatches methods by string name using reflection.
// Static AST analysis cannot resolve which concrete method will be invoked at runtime.
type DynamicDispatcher struct {
	handlers map[string]interface{}
}

func NewDynamicDispatcher() *DynamicDispatcher {
	return &DynamicDispatcher{
		handlers: make(map[string]interface{}),
	}
}

func (d *DynamicDispatcher) Register(name string, handler interface{}) {
	d.handlers[name] = handler
}

func (d *DynamicDispatcher) Invoke(name string, args ...interface{}) (interface{}, error) {
	handler, exists := d.handlers[name]
	if !exists {
		return nil, fmt.Errorf("handler %s not found", name)
	}

	val := reflect.ValueOf(handler)
	if val.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler %s is not a function", name)
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		in[i] = reflect.ValueOf(arg)
	}

	results := val.Call(in)
	if len(results) > 0 {
		return results[0].Interface(), nil
	}
	return nil, nil
}
