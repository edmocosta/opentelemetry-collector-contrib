// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ottl // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// PathExpressionParser is how a context provides OTTL access to all its Paths.
type PathExpressionParser[K any] func(Path[K]) (GetSetter[K], error)

// EnumParser is how a context provides OTTL access to all its Enums.
type EnumParser func(*EnumSymbol) (*Enum, error)

// Enum is how OTTL represents an enum's numeric value.
type Enum int64

// EnumSymbol is how OTTL represents an enum's string value.
type EnumSymbol string

func buildOriginalText(path *path) string {
	var builder strings.Builder
	if path.Context != "" {
		builder.WriteString(path.Context)
		if len(path.Fields) > 0 {
			builder.WriteString(".")
		}
	}
	for i, f := range path.Fields {
		builder.WriteString(f.Name)
		if len(f.Keys) > 0 {
			builder.WriteString(buildOriginalKeysText(f.Keys))
		}
		if i != len(path.Fields)-1 {
			builder.WriteString(".")
		}
	}
	return builder.String()
}

func buildOriginalKeysText(keys []key) string {
	var builder strings.Builder
	if len(keys) > 0 {
		for _, k := range keys {
			builder.WriteString("[")
			if k.Int != nil {
				builder.WriteString(strconv.FormatInt(*k.Int, 10))
			}
			if k.String != nil {
				builder.WriteString(*k.String)
			}
			if k.Expression != nil {
				if k.Expression.Path != nil {
					builder.WriteString(buildOriginalText(k.Expression.Path))
				}
				if k.Expression.Float != nil {
					builder.WriteString(strconv.FormatFloat(*k.Expression.Float, 'f', 10, 64))
				}
				if k.Expression.Int != nil {
					builder.WriteString(strconv.FormatInt(*k.Expression.Int, 10))
				}
			}
			builder.WriteString("]")
		}
	}
	return builder.String()
}

func (p *Parser[K]) newPath(path *path) (*basePath[K], error) {
	if len(path.Fields) == 0 {
		return nil, errors.New("cannot make a path from zero fields")
	}

	pathContext, fields, err := p.parsePathContext(path)
	if err != nil {
		return nil, err
	}

	originalText := buildOriginalText(path)
	var current *basePath[K]
	for i := len(fields) - 1; i >= 0; i-- {
		keys, err := p.newKeys(fields[i].Keys)
		if err != nil {
			return nil, err
		}
		current = &basePath[K]{
			context:      pathContext,
			name:         fields[i].Name,
			keys:         keys,
			nextPath:     current,
			originalText: originalText,
		}
	}
	current.fetched = true
	current.originalText = originalText
	return current, nil
}

func (p *Parser[K]) parsePathContext(path *path) (string, []field, error) {
	hasPathContextNames := len(p.pathContextNames) > 0
	if path.Context != "" {
		// no pathContextNames means the Parser isn't handling the grammar path's context yet,
		// so it falls back to the previous behavior with the path.Context value as the first
		// path's segment.
		if !hasPathContextNames {
			return "", append([]field{{Name: path.Context}}, path.Fields...), nil
		}

		if _, ok := p.pathContextNames[path.Context]; !ok {
			return "", path.Fields, fmt.Errorf(`context "%s" from path "%s" is not valid, it must be replaced by one of: %s`, path.Context, buildOriginalText(path), p.buildPathContextNamesText(""))
		}

		return path.Context, path.Fields, nil
	}

	if hasPathContextNames {
		originalText := buildOriginalText(path)
		return "", nil, fmt.Errorf(`missing context name for path "%s", possibly valid options are: %s`, originalText, p.buildPathContextNamesText(originalText))
	}

	return "", path.Fields, nil
}

func (p *Parser[K]) buildPathContextNamesText(path string) string {
	var builder strings.Builder
	var suffix string
	if path != "" {
		suffix = "." + path
	}

	i := 0
	for ctx := range p.pathContextNames {
		builder.WriteString(fmt.Sprintf(`"%s%s"`, ctx, suffix))
		if i != len(p.pathContextNames)-1 {
			builder.WriteString(", ")
		}
		i++
	}
	return builder.String()
}

// Path represents a chain of path parts in an OTTL statement, such as `body.string`.
// A Path has a name, and potentially a set of keys.
// If the path in the OTTL statement contains multiple parts (separated by a dot (`.`)), then the Path will have a pointer to the next Path.
type Path[K any] interface {
	// Context is the OTTL context name of this Path.
	Context() string

	// Name is the name of this segment of the path.
	Name() string

	// Next provides the next path segment for this Path.
	// Will return nil if there is no next path.
	Next() Path[K]

	// Keys provides the Keys for this Path.
	// Will return nil if there are no Keys.
	Keys() []Key[K]

	// String returns a string representation of this Path and the next Paths
	String() string
}

var _ Path[any] = &basePath[any]{}

type basePath[K any] struct {
	context      string
	name         string
	keys         []Key[K]
	nextPath     *basePath[K]
	fetched      bool
	fetchedKeys  bool
	originalText string
}

func (p *basePath[K]) Context() string {
	return p.context
}

func (p *basePath[K]) Name() string {
	return p.name
}

func (p *basePath[K]) Next() Path[K] {
	if p.nextPath == nil {
		return nil
	}
	p.nextPath.fetched = true
	return p.nextPath
}

func (p *basePath[K]) Keys() []Key[K] {
	if p.keys == nil {
		return nil
	}
	p.fetchedKeys = true
	return p.keys
}

func (p *basePath[K]) String() string {
	return p.originalText
}

func (p *basePath[K]) isComplete() error {
	if !p.fetched {
		return fmt.Errorf("the path section %q was not used by the context - this likely means you are using extra path sections", p.name)
	}
	if p.keys != nil && !p.fetchedKeys {
		return fmt.Errorf("the keys indexing %q were not used by the context - this likely means you are trying to index a path that does not support indexing", p.name)
	}
	if p.nextPath == nil {
		return nil
	}
	return p.nextPath.isComplete()
}

func (p *Parser[K]) newKeys(keys []key) ([]Key[K], error) {
	if len(keys) == 0 {
		return nil, nil
	}
	ks := make([]Key[K], len(keys))
	for i := range keys {
		var getter Getter[K]
		if keys[i].Expression != nil {
			if keys[i].Expression.Path != nil {
				g, err := p.buildGetSetterFromPath(keys[i].Expression.Path)
				if err != nil {
					return nil, err
				}
				getter = g
			}
			if keys[i].Expression.Converter != nil {
				g, err := p.newGetterFromConverter(*keys[i].Expression.Converter)
				if err != nil {
					return nil, err
				}
				getter = g
			}
		}
		if keys[i].MathExpression != nil {
			g, err := p.evaluateMathExpression(keys[i].MathExpression)
			if err != nil {
				return nil, err
			}
			getter = g
		}
		ks[i] = &baseKey[K]{
			s: keys[i].String,
			i: keys[i].Int,
			g: getter,
		}
	}
	return ks, nil
}

// Key represents a chain of keys in an OTTL statement, such as `attributes["foo"]["bar"]`.
// A Key has a String or Int, and potentially the next Key.
// If the path in the OTTL statement contains multiple keys, then the Key will have a pointer to the next Key.
type Key[K any] interface {
	// String returns a pointer to the Key's string value.
	// If the Key does not have a string value the returned value is nil.
	// If Key experiences an error retrieving the value it is returned.
	String(context.Context, K) (*string, error)

	// Int returns a pointer to the Key's int value.
	// If the Key does not have a int value the returned value is nil.
	// If Key experiences an error retrieving the value it is returned.
	Int(context.Context, K) (*int64, error)

	// ExpressionGetter returns a Getter to the expression, that can be
	// part of the path.
	// If the Key does not have an expression the returned value is nil.
	// If Key experiences an error retrieving the value it is returned.
	ExpressionGetter(context.Context, K) (Getter[K], error)
}

var _ Key[any] = &baseKey[any]{}

type baseKey[K any] struct {
	s *string
	i *int64
	g Getter[K]
}

func (k *baseKey[K]) String(_ context.Context, _ K) (*string, error) {
	return k.s, nil
}

func (k *baseKey[K]) Int(_ context.Context, _ K) (*int64, error) {
	return k.i, nil
}

func (k *baseKey[K]) ExpressionGetter(_ context.Context, _ K) (Getter[K], error) {
	return k.g, nil
}

func (p *Parser[K]) parsePath(ip *basePath[K]) (GetSetter[K], error) {
	g, err := p.pathParser(ip)
	if err != nil {
		return nil, err
	}
	err = ip.isComplete()
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (p *Parser[K]) newFunctionCall(ed editor) (Expr[K], error) {
	f, ok := p.functions[ed.Function]
	if !ok {
		return Expr[K]{}, fmt.Errorf("undefined function %q", ed.Function)
	}
	defaultArgs := f.CreateDefaultArguments()
	var args Arguments

	// A nil value indicates the function takes no arguments.
	if defaultArgs != nil {
		// Pointer values are necessary to fulfill the Go reflection
		// settability requirements. Non-pointer values are not
		// modifiable through reflection.
		if reflect.TypeOf(defaultArgs).Kind() != reflect.Pointer {
			return Expr[K]{}, fmt.Errorf("factory for %q must return a pointer to an Arguments value in its CreateDefaultArguments method", ed.Function)
		}

		args = reflect.New(reflect.ValueOf(defaultArgs).Elem().Type()).Interface()

		err := p.buildArgs(ed, reflect.ValueOf(args).Elem())
		if err != nil {
			return Expr[K]{}, fmt.Errorf("error while parsing arguments for call to %q: %w", ed.Function, err)
		}
	}

	fn, err := f.CreateFunction(FunctionContext{Set: p.telemetrySettings}, args)
	if err != nil {
		return Expr[K]{}, fmt.Errorf("couldn't create function: %w", err)
	}

	return Expr[K]{exprFunc: fn}, err
}

func (p *Parser[K]) buildArgs(ed editor, argsVal reflect.Value) error {
	requiredArgs := 0
	seenNamed := false

	for i := 0; i < len(ed.Arguments); i++ {
		if !seenNamed && ed.Arguments[i].Name != "" {
			seenNamed = true
		} else if seenNamed && ed.Arguments[i].Name == "" {
			return errors.New("unnamed argument used after named argument")
		}
	}

	for i := 0; i < argsVal.NumField(); i++ {
		if !strings.HasPrefix(argsVal.Field(i).Type().Name(), "Optional") {
			requiredArgs++
		}
	}

	if len(ed.Arguments) < requiredArgs || len(ed.Arguments) > argsVal.NumField() {
		return fmt.Errorf("incorrect number of arguments. Expected: %d Received: %d", argsVal.NumField(), len(ed.Arguments))
	}

	for i, edArg := range ed.Arguments {
		var field reflect.Value
		var fieldType reflect.Type
		var isOptional bool
		var arg argument

		if edArg.Name == "" {
			field = argsVal.Field(i)
			fieldType = field.Type()
			isOptional = strings.HasPrefix(fieldType.Name(), "Optional")
			arg = ed.Arguments[i]
		} else {
			field = argsVal.FieldByName(strcase.ToCamel(edArg.Name))
			if !field.IsValid() {
				return fmt.Errorf("no such parameter: %s", edArg.Name)
			}
			fieldType = field.Type()
			isOptional = strings.HasPrefix(fieldType.Name(), "Optional")
			arg = edArg
		}

		var val any
		var optionalFieldRef typedValueWrapper
		var err error
		var ok bool
		if isOptional {
			optionalFieldRef, ok = field.Addr().Interface().(typedValueWrapper)
			if !ok {
				return errors.New("optional type is not manageable by the OTTL parser. This is an error in the OTTL")
			}
			fieldType = optionalFieldRef.getWrappedType()
		}

		switch {
		case strings.HasPrefix(fieldType.Name(), "FunctionGetter"):
			var name string
			switch {
			case arg.Value.Enum != nil:
				name = string(*arg.Value.Enum)
			case arg.FunctionName != nil:
				name = *arg.FunctionName
			default:
				return errors.New("invalid function name given")
			}
			var f Factory[K]
			f, ok = p.functions[name]
			if !ok {
				return fmt.Errorf("undefined function %s", name)
			}
			val = StandardFunctionGetter[K]{FCtx: FunctionContext{Set: p.telemetrySettings}, Fact: f}
		case strings.HasPrefix(fieldType.Name(), "SliceGetter"):
			var fieldVal typedValueWrapper
			if isOptional {
				fieldVal, ok = optionalFieldRef.getRawValue().(typedValueWrapper)
			} else {
				fieldVal, ok = field.Addr().Interface().(typedValueWrapper)
			}
			if !ok {
				return errors.New("SliceGetter is not a typedValueWrapper. This is a bug in the OTTL")
			}

			get, err := createStandardSliceGetter[K](
				arg.Value,
				fieldVal.getWrappedType(),
				p.buildSliceArg,
				p.buildStandardGetterSetter,
				p.newGetter,
			)
			if err != nil {
				return err
			}

			err = fieldVal.setWrappedValue(reflect.ValueOf(get))
			if err != nil {
				return err
			}
			val = reflect.ValueOf(fieldVal).Elem().Interface()

		case strings.HasPrefix(fieldType.Name(), "LiteralGetter"):
			var fieldVal typedValueWrapper
			if isOptional {
				fieldVal, ok = optionalFieldRef.getRawValue().(typedValueWrapper)
			} else {
				fieldVal, ok = field.Addr().Interface().(typedValueWrapper)
			}
			if !ok {
				return errors.New("LiteralGetter is not a typedValueWrapper. This is a bug in the OTTL")
			}

			var buildArg any
			buildArg, err = p.buildArg(arg.Value, fieldVal.getWrappedType())
			if err != nil {
				return err
			}

			if _, ok = buildArg.(literalGetter); !ok {
				return fmt.Errorf("getter type %T does not support literal values", buildArg)
			}

			err = fieldVal.setWrappedValue(reflect.ValueOf(buildArg))
			if err != nil {
				return err
			}
			val = reflect.ValueOf(fieldVal).Elem().Interface()
		case fieldType.Kind() == reflect.Slice:
			val, err = p.buildSliceArg(arg.Value, fieldType)
		default:
			val, err = p.buildArg(arg.Value, fieldType)
		}
		if err != nil {
			return fmt.Errorf("invalid argument at position %v: %w", i, err)
		}
		if isOptional {
			err = optionalFieldRef.setWrappedValue(reflect.ValueOf(val))
			if err != nil {
				return err
			}
		} else {
			field.Set(reflect.ValueOf(val))
		}
	}

	return nil
}

func (p *Parser[K]) buildSliceArg(argVal value, argType reflect.Type) (any, error) {
	name := argType.Elem().Name()
	switch {
	case name == reflect.Uint8.String():
		if argVal.Bytes == nil {
			return nil, errors.New("slice parameter must be a byte slice literal")
		}
		return []byte(*argVal.Bytes), nil
	case name == reflect.String.String():
		arg, err := buildSlice[string](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case name == reflect.Float64.String():
		arg, err := buildSlice[float64](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case name == reflect.Int64.String():
		arg, err := buildSlice[int64](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "Getter"):
		arg, err := buildSlice[Getter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "PMapGetter"):
		arg, err := buildSlice[PMapGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "PSliceGetter"):
		arg, err := buildSlice[PSliceGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "StringGetter"):
		arg, err := buildSlice[StringGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "StringLikeGetter"):
		arg, err := buildSlice[StringLikeGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "FloatGetter"):
		arg, err := buildSlice[FloatGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "FloatLikeGetter"):
		arg, err := buildSlice[FloatLikeGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "IntGetter"):
		arg, err := buildSlice[IntGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "IntLikeGetter"):
		arg, err := buildSlice[IntLikeGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "DurationGetter"):
		arg, err := buildSlice[DurationGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	case strings.HasPrefix(name, "TimeGetter"):
		arg, err := buildSlice[TimeGetter[K]](argVal, argType, p.buildArg, name)
		if err != nil {
			return nil, err
		}
		return arg, nil
	default:
		return nil, fmt.Errorf("unsupported slice type %q for function", argType.Elem().Name())
	}
}

func (p *Parser[K]) buildGetSetterFromPath(path *path) (GetSetter[K], error) {
	np, err := p.newPath(path)
	if err != nil {
		return nil, err
	}
	arg, err := p.parsePath(np)
	if err != nil {
		return nil, err
	}
	return arg, nil
}

func (p *Parser[K]) buildStandardGetterSetter(name string, getter Getter[K]) (any, error) {
	switch {
	case strings.HasPrefix(name, "Setter"),
		strings.HasPrefix(name, "GetSetter"),
		strings.HasPrefix(name, "Getter"):
		return getter, nil
	case strings.HasPrefix(name, "StringGetter"):
		return newStandardStringGetter(getter), nil
	case strings.HasPrefix(name, "StringLikeGetter"):
		return newStandardStringLikeGetter[K](getter), nil
	case strings.HasPrefix(name, "FloatGetter"):
		return newStandardFloatGetter(getter), nil
	case strings.HasPrefix(name, "FloatLikeGetter"):
		return newStandardFloatLikeGetter(getter), nil
	case strings.HasPrefix(name, "IntGetter"):
		return newStandardIntGetter(getter), nil
	case strings.HasPrefix(name, "IntLikeGetter"):
		return newStandardIntLikeGetter(getter), nil
	case strings.HasPrefix(name, "PMapGetSetter"):
		stdMapGetter := StandardPMapGetter[K]{Getter: getter.Get}
		if setter, ok := getter.(Setter[K]); ok {
			return StandardPMapGetSetter[K]{Getter: stdMapGetter.Get, Setter: setter.Set}, nil
		}
		return nil, fmt.Errorf("type %q is not a setter and cannot be used as PMapGetSetter", name)
	case strings.HasPrefix(name, "PMapGetter"):
		return newStandardPMapGetter(getter), nil
	case strings.HasPrefix(name, "PSliceGetter"):
		return newStandardPSliceGetter(getter), nil
	case strings.HasPrefix(name, "DurationGetter"):
		return newStandardDurationGetter(getter), nil
	case strings.HasPrefix(name, "TimeGetter"):
		return newStandardTimeGetter(getter), nil
	case strings.HasPrefix(name, "BoolGetter"):
		return newStandardBoolGetter(getter), nil
	case strings.HasPrefix(name, "BoolLikeGetter"):
		return newStandardBoolLikeGetter(getter), nil
	case strings.HasPrefix(name, "ByteSliceLikeGetter"):
		return newStandardByteSliceLikeGetter(getter), nil
	}
	return nil, fmt.Errorf("unsupported argument type: %s", name)
}

// Handle interfaces that can be passed as arguments to OTTL functions.
func (p *Parser[K]) buildArg(argVal value, argType reflect.Type) (any, error) {
	name := argType.Name()
	switch {
	case name == "Enum":
		arg, err := p.enumParser((*EnumSymbol)(argVal.Enum))
		if err != nil {
			return nil, errors.New("must be an Enum")
		}
		return *arg, nil
	case name == reflect.String.String():
		if argVal.String == nil {
			return nil, errors.New("must be a string")
		}
		return *argVal.String, nil
	case name == reflect.Float64.String():
		if argVal.Literal == nil || argVal.Literal.Float == nil {
			return nil, errors.New("must be a float")
		}
		return *argVal.Literal.Float, nil
	case name == reflect.Int64.String():
		if argVal.Literal == nil || argVal.Literal.Int == nil {
			return nil, errors.New("must be an int")
		}
		return *argVal.Literal.Int, nil
	case name == reflect.Bool.String():
		if argVal.Bool == nil {
			return nil, errors.New("must be a bool")
		}
		return bool(*argVal.Bool), nil
	case strings.HasPrefix(name, "Setter"),
		strings.HasPrefix(name, "GetSetter"),
		strings.HasPrefix(name, "PMapGetSetter"):
		if argVal.Literal == nil || argVal.Literal.Path == nil {
			return nil, errors.New("must be a path")
		}
		gs, err := p.buildGetSetterFromPath(argVal.Literal.Path)
		if err != nil {
			return nil, err
		}
		return p.buildStandardGetterSetter(name, gs)
	default:
		arg, err := p.newGetter(argVal)
		if err != nil {
			return nil, err
		}
		gs, err := p.buildStandardGetterSetter(name, arg)
		if err != nil {
			return nil, err
		}
		return gs, nil
	}
}

type buildArgFunc func(value, reflect.Type) (any, error)

func buildSlice[T any](argVal value, argType reflect.Type, buildArg buildArgFunc, name string) ([]T, error) {
	if argVal.List == nil {
		return nil, fmt.Errorf("must be a list of type %v", name)
	}

	vals := make([]T, 0, len(argVal.List.Values))
	values := argVal.List.Values
	for j := 0; j < len(values); j++ {
		untypedVal, err := buildArg(values[j], argType.Elem())
		if err != nil {
			return nil, fmt.Errorf("error while parsing list argument at index %v: %w", j, err)
		}

		val, ok := untypedVal.(T)

		if !ok {
			return nil, fmt.Errorf("invalid element type at list index %v, must be of type %v", j, name)
		}

		vals = append(vals, val)
	}

	return vals, nil
}

// typedValueWrapper is an interface that allows types to wrap values with generic types,
// and access the type information at runtime.
type typedValueWrapper interface {
	// getWrappedType returns the type of the wrapped value.
	getWrappedType() reflect.Type
	// setWrappedValue sets the wrapped value to the provided reflect.Value.
	setWrappedValue(val reflect.Value) error
	// getRawValue returns the raw value of the wrapped type, if applicable, it should
	// return a pointer to the value so it can be modified externally.
	getRawValue() any
}

// Ensure Optional implements typedValueWrapper.
var _ typedValueWrapper = (*Optional[any])(nil)

// Optional is used to represent an optional function argument
type Optional[T any] struct {
	val      T
	hasValue bool
}

func (*Optional[T]) getWrappedType() reflect.Type {
	return reflect.TypeFor[T]()
}

func (o *Optional[T]) setWrappedValue(val reflect.Value) error {
	typedVal, ok := val.Interface().(T)
	if !ok {
		return fmt.Errorf("cannot set value of type %q to an Optional of type %q", val.Type(), reflect.TypeFor[T]())
	}
	o.val = typedVal
	o.hasValue = true
	return nil
}

func (o *Optional[T]) getRawValue() any {
	return &o.val
}

// IsEmpty returns true if the Optional[T] does not contain a value.
func (o *Optional[T]) IsEmpty() bool {
	return !o.hasValue
}

// Get returns the value contained in the Optional[T].
func (o *Optional[T]) Get() T {
	return o.val
}

// GetOr returns the value contained in the Optional[T] if it exists,
// otherwise it returns the default value provided.
func (o *Optional[T]) GetOr(value T) T {
	if !o.hasValue {
		return value
	}
	return o.val
}

// NewTestingOptional allows creating an Optional with a value already populated for use in testing
// OTTL functions.
func NewTestingOptional[T any](val T) Optional[T] {
	return Optional[T]{
		val:      val,
		hasValue: true,
	}
}

// typedGetter is like Getter, but with typed return values.
type typedGetter[K, V any] interface {
	Get(ctx context.Context, tCtx K) (V, error)
}

// Ensure LiteralGetter implements typedValueWrapper.
var _ typedValueWrapper = (*LiteralGetter[any, any, typedGetter[any, any]])(nil)

// LiteralGetter allows OTTL functions to use getters that might return literal
// values. It provides a way to check if the getter is a literal getter and to retrieve
// the literal value from it.
// K is the type of the context, G is the type of the getter, and V is the type of
// the literal value returned by getter G.
type LiteralGetter[K, V any, G typedGetter[K, V]] struct {
	getter G
}

func (*LiteralGetter[K, V, G]) getWrappedType() reflect.Type {
	return reflect.TypeFor[G]()
}

func (p *LiteralGetter[K, V, G]) setWrappedValue(val reflect.Value) error {
	typedVal, ok := val.Interface().(G)
	if !ok {
		return fmt.Errorf("cannot set value of type %s to a Getter of type %s", val.Type(), reflect.TypeFor[G]())
	}
	p.getter = typedVal
	return nil
}

func (p *LiteralGetter[K, V, G]) getRawValue() any {
	return p.getter
}

// IsLiteral checks if the wrapped getter holds a literal getter.
func (p *LiteralGetter[K, G, V]) IsLiteral() bool {
	lg, ok := any(p.getter).(literalGetter)
	return ok && lg.isLiteral()
}

// GetLiteral retrieves the literal value from the getter.
// If the getter is not a literal getter, or the value it's holding is not a literal,
// an error is returned.
func (p *LiteralGetter[K, V, G]) GetLiteral() (V, error) {
	lg, ok := any(p.getter).(literalGetter)
	if !ok {
		return *new(V), fmt.Errorf("getter of type %T is not a literal getter", p.getter)
	}
	val, err := lg.getLiteral()
	if err != nil {
		return *new(V), err
	}
	if typedVal, ok := val.(V); ok {
		return typedVal, nil
	}
	// should not happen thanks to the type restriction
	return *new(V), fmt.Errorf("value is not of expected type %T, got %T instead", reflect.TypeFor[V](), val)
}

func (p *LiteralGetter[K, V, G]) Get(ctx context.Context, tCtx K) (V, error) {
	return p.getter.Get(ctx, tCtx)
}

// mockLiteralGetter is a mock implementation of LiteralGetter that can be used for testing.
type mockLiteralGetter[K, V any] struct {
	valueGetter func(context.Context, K) (V, error)
	literal     bool
}

func (m mockLiteralGetter[K, V]) Get(_ context.Context, _ K) (V, error) {
	return m.valueGetter(context.Background(), *new(K))
}

func (m mockLiteralGetter[K, V]) isLiteral() bool {
	return m.literal
}

func (m mockLiteralGetter[K, V]) getLiteral() (any, error) {
	return m.valueGetter(context.Background(), *new(K))
}

// NewTestingLiteralGetter allows creating an LiteralGetter with a getter already populated
// for use in testing OTTL functions.
func NewTestingLiteralGetter[K, V any, G typedGetter[K, V]](literal bool, getter G) LiteralGetter[K, V, G] {
	mockedGetter := mockLiteralGetter[K, V]{
		valueGetter: getter.Get,
		literal:     literal,
	}
	return LiteralGetter[K, V, G]{getter: any(mockedGetter).(G)}
}

// Ensure SliceGetter implements typedValueWrapper.
var _ typedValueWrapper = (*SliceGetter[any, any])(nil)

type SliceGetter[K, T any] struct {
	literal bool
	getter  Getter[K]
}

func (s *SliceGetter[K, T]) isLiteral() bool {
	lg, ok := s.getter.(literalGetter)
	return ok && lg.isLiteral()
}

func (s *SliceGetter[K, T]) getLiteral() (any, error) {
	if !s.literal {
		return nil, errors.New("SliceGetter value is not a literal")
	}
	return s.Get(context.Background(), *new(K))
}

func (*SliceGetter[K, T]) getWrappedType() reflect.Type {
	return reflect.TypeFor[T]()
}

func (s *SliceGetter[K, T]) getRawValue() any {
	return s.getter
}

func (s *SliceGetter[K, T]) setWrappedValue(val reflect.Value) error {
	if typedVal, ok := val.Interface().(Getter[K]); ok {
		s.getter = typedVal
		return nil
	}
	return fmt.Errorf("cannot set value of type %s to a SliceGetter of type %s", val.Type(), reflect.TypeFor[T]())
}

func (s *SliceGetter[K, T]) Get(ctx context.Context, tCtx K) ([]T, error) {
	val, err := s.getter.Get(ctx, tCtx)
	if err != nil {
		return nil, err
	}
	if ts, ok := val.([]T); ok {
		return ts, nil
	}

	sliceVals, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid slice value; expected a slice of %s, got %T", reflect.TypeFor[T](), val)
	}

	res := make([]T, 0, len(sliceVals))
	for _, v := range sliceVals {
		vt, ok := v.(T)
		if !ok {
			return nil, fmt.Errorf("invalid slice item type: %T, expected %s", v, reflect.TypeFor[T]())
		}
		res = append(res, vt)
	}
	return res, nil
}

type standardGetter[K any] struct {
	getter  func(ctx context.Context, tCtx K) (any, error)
	literal bool
}

func (s standardGetter[K]) Get(ctx context.Context, tCtx K) (any, error) {
	return s.getter(ctx, tCtx)
}

func (s standardGetter[K]) isLiteral() bool {
	return s.literal
}

func (s standardGetter[K]) getLiteral() (any, error) {
	return s.Get(context.Background(), *new(K))
}

func createStandardSliceGetter[K any](
	argVal value,
	sliceItemType reflect.Type,
	buildSliceArg func(value, reflect.Type) (any, error),
	buildSliceItemGetter func(string, Getter[K]) (any, error),
	buildGetter func(value) (Getter[K], error),
) (Getter[K], error) {
	var valueGetter func(context.Context, K) (any, error)
	var isLiteral bool

	switch sliceItemType.Kind() {
	case reflect.String, reflect.Uint8, reflect.Float64, reflect.Int64:
		isLiteral = true
	default:
		isLiteral = false
	}

	if argVal.List != nil {
		slice, err := buildSliceArg(argVal, reflect.SliceOf(sliceItemType))
		if err != nil {
			return nil, err
		}

		sliceValue := reflect.ValueOf(slice)
		if !isLiteral && sliceValue.Kind() == reflect.Slice && sliceValue.Len() > 0 {
			allLiterals := true
			for i := 0; i < sliceValue.Len(); i++ {
				rawVal := sliceValue.Index(i).Interface()
				if litGetter, ok := rawVal.(literalGetter); !ok || !litGetter.isLiteral() {
					allLiterals = false
					break
				}
			}
			isLiteral = allLiterals
		}

		valueGetter = func(context.Context, K) (any, error) { return slice, nil }
	} else {
		argValGetter, err := buildGetter(argVal)
		if err != nil {
			return nil, err
		}
		if litGetter, ok := argValGetter.(literalGetter); ok && litGetter.isLiteral() {
			isLiteral = true
			litValues, err := litGetter.getLiteral()
			if err != nil {
				return nil, err
			}
			valueGetter = func(context.Context, K) (any, error) { return litValues, nil }
		} else {
			isLiteral = false
			valueGetter = argValGetter.Get
		}
	}

	return standardGetter[K]{
		literal: isLiteral,
		getter: func(ctx context.Context, tCtx K) (any, error) {
			var res []any
			values, err := valueGetter(ctx, tCtx)
			if err != nil {
				return nil, err
			}

			switch typedVal := values.(type) {
			case pcommon.Slice:
				values = typedVal.AsRaw()
			case pcommon.Value:
				if typedVal.Type() != pcommon.ValueTypeSlice {
					return nil, fmt.Errorf("expected a slice, got %q", typedVal.Type())
				}
				values = typedVal.Slice().AsRaw()
			default:
				valuesType := reflect.TypeOf(values)
				if valuesType == reflect.SliceOf(sliceItemType) {
					return values, nil
				}
				if valuesType.Kind() != reflect.Slice {
					return nil, fmt.Errorf("expected a slice, got %q", reflect.TypeOf(values).Kind())
				}
			}

			sliceValue := reflect.ValueOf(values)
			for i := 0; i < sliceValue.Len(); i++ {
				rawVal := sliceValue.Index(i)
				if rawVal.Type() == sliceItemType {
					res = append(res, rawVal.Interface())
				} else {
					itemGetter, err := buildSliceItemGetter(sliceItemType.Name(), literal[K]{value: rawVal.Interface()})
					if err != nil {
						return nil, err
					}
					res = append(res, itemGetter)
				}
			}
			return res, nil
		},
	}, nil
}
