package toolexec

import (
	"encoding/json"
	"fmt"
	"reflect"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/jsonschema-go/jsonschema"
)

// Define builds a tool whose arguments decode into T and whose parameter
// schema is inferred from T. Struct tags carry what the schema needs: the
// json tag names the property and marks it optional with omitempty, the
// jsonschema tag is its description, and a pointer field reads back as nil
// when the model omitted it. Constraints the tags cannot express — minimum,
// maximum, enum — are applied by the shape functions (Range, Enum, ...).
//
// The inferred schema is trimmed to the form Memoh's hand-written schemas
// used (see normalizeSchema), so a tool that moves onto a typed handler keeps
// the definition the model has been reading. internal/agent/tool pins that
// with a golden test per provider.
func Define[T any](name, description string, execute func(*ToolExecContext, T) (sdk.ToolOutput, error), shape ...func(*jsonschema.Schema)) Tool {
	return Tool{
		Name:        name,
		Description: description,
		Parameters:  SchemaFor[T](shape...),
		Execute:     Typed(execute),
	}
}

// SchemaFor infers the parameter schema of T (see Define) and applies the
// shape functions. It panics on a type the schema cannot express: the tools
// are built at startup, so that is a programming error, not a runtime one.
func SchemaFor[T any](shape ...func(*jsonschema.Schema)) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("toolexec: cannot infer schema for %T: %v", *new(T), err))
	}
	normalizeSchema(schema)
	for _, fn := range shape {
		fn(schema)
	}
	return schema
}

// Typed adapts a handler over T to the executor contract: the arguments are
// decoded into T before the handler runs, and a document that does not decode
// is reported as the tool's error.
func Typed[T any](execute func(*ToolExecContext, T) (sdk.ToolOutput, error)) ToolExecuteFunc {
	return func(ctx *ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
		var typed T
		if err := input.Unmarshal(&typed); err != nil {
			return sdk.ToolOutput{}, fmt.Errorf("decode %s arguments: %w", ctx.ToolName, err)
		}
		return execute(ctx, typed)
	}
}

// normalizeSchema removes what jsonschema.For adds beyond the object /
// properties / required form: the additionalProperties:false every struct
// gets, the nullable type pair every pointer and slice gets, and the
// property order extension. An object always carries a properties member,
// even when empty. Nested schemas are normalized the same way.
func normalizeSchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if s.AdditionalProperties != nil && isFalseSchema(s.AdditionalProperties) {
		s.AdditionalProperties = nil
	}
	s.PropertyOrder = nil
	if len(s.Types) == 2 && s.Types[0] == "null" {
		s.Type = s.Types[1]
		s.Types = nil
	}
	if s.Type == "object" && s.Properties == nil {
		s.Properties = map[string]*jsonschema.Schema{}
	}
	for _, property := range s.Properties {
		normalizeSchema(property)
	}
	normalizeSchema(s.Items)
	normalizeSchema(s.AdditionalProperties)
}

// isFalseSchema recognises the schema jsonschema.For uses to disallow
// additional properties: an empty "not".
func isFalseSchema(s *jsonschema.Schema) bool {
	return s != nil && s.Not != nil && reflect.DeepEqual(*s.Not, jsonschema.Schema{})
}

// Property returns the schema of one top-level property, panicking when the
// struct has no such field: a constraint naming a missing property is a
// programming error, caught when the provider is built.
func Property(s *jsonschema.Schema, name string) *jsonschema.Schema {
	property, ok := s.Properties[name]
	if !ok {
		panic(fmt.Sprintf("toolexec: schema has no property %q", name))
	}
	return property
}

// Range bounds a numeric property with minimum and maximum.
func Range(name string, minimum, maximum float64) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		p := Property(s, name)
		p.Minimum = &minimum
		p.Maximum = &maximum
	}
}

// Minimum bounds a numeric property from below.
func Minimum(name string, minimum float64) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { Property(s, name).Minimum = &minimum }
}

// Maximum bounds a numeric property from above.
func Maximum(name string, maximum float64) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { Property(s, name).Maximum = &maximum }
}

// Enum restricts a property to the listed values.
func Enum(name string, values ...any) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { Property(s, name).Enum = values }
}

// Items applies shape functions to the item schema of an array property.
func Items(name string, shape ...func(*jsonschema.Schema)) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		items := Property(s, name).Items
		if items == nil {
			panic(fmt.Sprintf("toolexec: property %q is not an array", name))
		}
		for _, fn := range shape {
			fn(items)
		}
	}
}

// Replace substitutes the inferred schema of one property with an explicit
// one, for a shape inference cannot produce (a nullable union, a custom
// decoder type).
func Replace(name string, schema *jsonschema.Schema) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		Property(s, name)
		s.Properties[name] = schema
	}
}

// Describe sets a property's description at build time, for text that
// depends on the session rather than on the struct.
func Describe(name, description string) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { Property(s, name).Description = description }
}

// Require replaces the required list, for tools whose required set depends
// on the session.
func Require(names ...string) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { s.Required = names }
}

// Default records a property's default for the model to read; the handler
// still applies it when the property is omitted.
func Default(name string, value any) func(*jsonschema.Schema) {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("toolexec: default for %q: %v", name, err))
	}
	return func(s *jsonschema.Schema) { Property(s, name).Default = raw }
}

// Strict disallows additional properties on the schema it is applied to.
func Strict() func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { s.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}} }
}

// ArrayBounds bounds the length of an array property.
func ArrayBounds(name string, minItems, maxItems int) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		p := Property(s, name)
		p.MinItems = &minItems
		p.MaxItems = &maxItems
	}
}

// EnumStrings is Enum for a list of strings built at runtime.
func EnumStrings(name string, values []string) func(*jsonschema.Schema) {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return Enum(name, out...)
}
