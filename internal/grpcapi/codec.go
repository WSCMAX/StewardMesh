// Requirements: REQ-API-001. Feature: integrations.protocols.
package grpcapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func messageObject(message proto.Message) (map[string]any, error) {
	if message == nil {
		return map[string]any{}, nil
	}
	value, err := messageValue(message.ProtoReflect())
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("protobuf request must be an object")
	}
	return object, nil
}

func messageValue(message protoreflect.Message) (any, error) {
	if message.Descriptor().FullName() == "google.protobuf.Timestamp" {
		value, ok := message.Interface().(*timestamppb.Timestamp)
		if !ok || !value.IsValid() {
			return nil, fmt.Errorf("timestamp is invalid")
		}
		return value.AsTime().UTC().Format(time.RFC3339Nano), nil
	}
	if message.Descriptor().FullName() == "google.protobuf.Struct" {
		value, ok := message.Interface().(*structpb.Struct)
		if !ok {
			return nil, fmt.Errorf("structured value is invalid")
		}
		return value.AsMap(), nil
	}
	result := make(map[string]any)
	var conversionError error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		converted, err := fieldValue(field, value)
		if err != nil {
			conversionError = fmt.Errorf("convert %s: %w", field.FullName(), err)
			return false
		}
		result[field.JSONName()] = converted
		return true
	})
	return result, conversionError
}

func fieldValue(field protoreflect.FieldDescriptor, value protoreflect.Value) (any, error) {
	if field.IsList() {
		list := value.List()
		result := make([]any, list.Len())
		for index := range result {
			converted, err := singularValue(field, list.Get(index))
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	}
	if field.IsMap() {
		result := make(map[string]any)
		var conversionError error
		value.Map().Range(func(key protoreflect.MapKey, item protoreflect.Value) bool {
			converted, err := singularValue(field.MapValue(), item)
			if err != nil {
				conversionError = err
				return false
			}
			result[key.String()] = converted
			return true
		})
		return result, conversionError
	}
	return singularValue(field, value)
}

func singularValue(field protoreflect.FieldDescriptor, value protoreflect.Value) (any, error) {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return value.Bool(), nil
	case protoreflect.EnumKind:
		return enumWireValue(field.Enum(), value.Enum()), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return value.Int(), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return json.Number(strconv.FormatInt(value.Int(), 10)), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return value.Uint(), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return json.Number(strconv.FormatUint(value.Uint(), 10)), nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return nil, fmt.Errorf("non-finite number is not supported")
		}
		return value.Float(), nil
	case protoreflect.StringKind:
		return value.String(), nil
	case protoreflect.BytesKind:
		return append([]byte(nil), value.Bytes()...), nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return messageValue(value.Message())
	default:
		return nil, fmt.Errorf("unsupported protobuf kind %s", field.Kind())
	}
}

func enumWireValue(descriptor protoreflect.EnumDescriptor, number protoreflect.EnumNumber) string {
	value := descriptor.Values().ByNumber(number)
	if value == nil || number == 0 {
		return ""
	}
	name := string(value.Name())
	prefix := upperSnake(string(descriptor.Name())) + "_"
	name = strings.TrimPrefix(name, prefix)
	result := strings.ToLower(name)
	if descriptor.FullName() == "stewardmesh.v1.BridgeScope" {
		result = strings.ReplaceAll(result, "_", ":")
	}
	return result
}

func upperSnake(value string) string {
	var result strings.Builder
	for index, current := range value {
		if unicode.IsUpper(current) && index > 0 {
			previous := rune(value[index-1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) {
				result.WriteByte('_')
			}
		}
		result.WriteRune(unicode.ToUpper(current))
	}
	return result.String()
}

func populateMessage(message protoreflect.Message, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("response must be a JSON object")
	}
	fields := message.Descriptor().Fields()
	for name, item := range object {
		field := fieldByJSONName(fields, name)
		if field == nil || item == nil {
			continue
		}
		if field.IsList() {
			items, ok := item.([]any)
			if !ok {
				return fmt.Errorf("decode response field %s: expected array, got %T", field.FullName(), item)
			}
			list := message.Mutable(field).List()
			for _, listItem := range items {
				converted, err := responseSingularValue(field, listItem)
				if err != nil {
					return fmt.Errorf("decode response field %s: %w", field.FullName(), err)
				}
				list.Append(converted)
			}
			continue
		}
		if field.IsMap() {
			items, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("decode response field %s: expected object, got %T", field.FullName(), item)
			}
			mapped := message.Mutable(field).Map()
			for key, mapItem := range items {
				converted, err := responseSingularValue(field.MapValue(), mapItem)
				if err != nil {
					return fmt.Errorf("decode response field %s: %w", field.FullName(), err)
				}
				mapped.Set(protoreflect.ValueOfString(key).MapKey(), converted)
			}
			continue
		}
		converted, err := responseSingularValue(field, item)
		if err != nil {
			return fmt.Errorf("decode response field %s: %w", field.FullName(), err)
		}
		message.Set(field, converted)
	}
	return nil
}

func fieldByJSONName(fields protoreflect.FieldDescriptors, name string) protoreflect.FieldDescriptor {
	if field := fields.ByJSONName(name); field != nil {
		return field
	}
	if field := fields.ByName(protoreflect.Name(name)); field != nil {
		return field
	}
	wanted := normalizedToken(name)
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if normalizedToken(field.JSONName()) == wanted || normalizedToken(string(field.Name())) == wanted {
			return field
		}
	}
	return nil
}

func responseSingularValue(field protoreflect.FieldDescriptor, value any) (protoreflect.Value, error) {
	switch field.Kind() {
	case protoreflect.BoolKind:
		parsed, err := boolValue(value)
		return protoreflect.ValueOfBool(parsed), err
	case protoreflect.EnumKind:
		number, err := enumNumber(field.Enum(), value)
		return protoreflect.ValueOfEnum(number), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		parsed, err := int64Value(value)
		if parsed < math.MinInt32 || parsed > math.MaxInt32 {
			return protoreflect.Value{}, fmt.Errorf("integer overflows int32")
		}
		return protoreflect.ValueOfInt32(int32(parsed)), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		parsed, err := int64Value(value)
		return protoreflect.ValueOfInt64(parsed), err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		parsed, err := uint64Value(value)
		if parsed > math.MaxUint32 {
			return protoreflect.Value{}, fmt.Errorf("integer overflows uint32")
		}
		return protoreflect.ValueOfUint32(uint32(parsed)), err
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		parsed, err := uint64Value(value)
		return protoreflect.ValueOfUint64(parsed), err
	case protoreflect.FloatKind:
		parsed, err := float64Value(value)
		return protoreflect.ValueOfFloat32(float32(parsed)), err
	case protoreflect.DoubleKind:
		parsed, err := float64Value(value)
		return protoreflect.ValueOfFloat64(parsed), err
	case protoreflect.StringKind:
		parsed, ok := value.(string)
		if !ok {
			return protoreflect.Value{}, fmt.Errorf("expected string, got %T", value)
		}
		return protoreflect.ValueOfString(parsed), nil
	case protoreflect.BytesKind:
		switch item := value.(type) {
		case []byte:
			return protoreflect.ValueOfBytes(append([]byte(nil), item...)), nil
		case string:
			decoded, err := base64.StdEncoding.DecodeString(item)
			if err != nil {
				return protoreflect.Value{}, fmt.Errorf("decode base64 bytes: %w", err)
			}
			return protoreflect.ValueOfBytes(decoded), nil
		default:
			return protoreflect.Value{}, fmt.Errorf("expected bytes, got %T", value)
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if field.Message().FullName() == "google.protobuf.Timestamp" {
			text, ok := value.(string)
			if !ok {
				return protoreflect.Value{}, fmt.Errorf("expected timestamp string, got %T", value)
			}
			parsed, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return protoreflect.Value{}, fmt.Errorf("parse timestamp: %w", err)
			}
			return protoreflect.ValueOfMessage(timestamppb.New(parsed.UTC()).ProtoReflect()), nil
		}
		if field.Message().FullName() == "google.protobuf.Struct" {
			object, ok := value.(map[string]any)
			if !ok {
				return protoreflect.Value{}, fmt.Errorf("expected structured object, got %T", value)
			}
			result, err := structpb.NewStruct(object)
			if err != nil {
				return protoreflect.Value{}, err
			}
			return protoreflect.ValueOfMessage(result.ProtoReflect()), nil
		}
		message := newDynamicMessage(field.Message())
		if err := populateMessage(message, value); err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfMessage(message), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported protobuf kind %s", field.Kind())
	}
}

func enumNumber(descriptor protoreflect.EnumDescriptor, value any) (protoreflect.EnumNumber, error) {
	if number, err := int64Value(value); err == nil {
		if descriptor.Values().ByNumber(protoreflect.EnumNumber(number)) != nil {
			return protoreflect.EnumNumber(number), nil
		}
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return 0, nil
	}
	wanted := normalizedToken(text)
	for index := 0; index < descriptor.Values().Len(); index++ {
		candidate := descriptor.Values().Get(index)
		if normalizedToken(enumWireValue(descriptor, candidate.Number())) == wanted || normalizedToken(string(candidate.Name())) == wanted {
			return candidate.Number(), nil
		}
	}
	return 0, fmt.Errorf("unknown %s value %q", descriptor.FullName(), text)
}

func normalizedToken(value string) string {
	var result strings.Builder
	for _, current := range strings.ToLower(value) {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			result.WriteRune(current)
		}
	}
	return result.String()
}

func boolValue(value any) (bool, error) {
	switch item := value.(type) {
	case bool:
		return item, nil
	case string:
		return strconv.ParseBool(item)
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

func int64Value(value any) (int64, error) {
	switch item := value.(type) {
	case json.Number:
		return item.Int64()
	case float64:
		if item != math.Trunc(item) || item < math.MinInt64 || item > math.MaxInt64 {
			return 0, fmt.Errorf("number is not an integer")
		}
		return int64(item), nil
	case int:
		return int64(item), nil
	case int32:
		return int64(item), nil
	case int64:
		return item, nil
	case uint32:
		return int64(item), nil
	case uint64:
		if item > math.MaxInt64 {
			return 0, fmt.Errorf("integer overflows int64")
		}
		return int64(item), nil
	case string:
		return strconv.ParseInt(item, 10, 64)
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func uint64Value(value any) (uint64, error) {
	parsed, err := int64Value(value)
	if err != nil || parsed < 0 {
		if err == nil {
			err = fmt.Errorf("integer must be non-negative")
		}
		return 0, err
	}
	return uint64(parsed), nil
}

func float64Value(value any) (float64, error) {
	switch item := value.(type) {
	case json.Number:
		return item.Float64()
	case float64:
		return item, nil
	case string:
		return strconv.ParseFloat(item, 64)
	default:
		parsed, err := int64Value(value)
		return float64(parsed), err
	}
}
