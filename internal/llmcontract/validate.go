package llmcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// ValidateJSON kiểm tra JSON thô có thoả tập con JSON Schema dùng cho hợp
// đồng trả về trực tiếp hay không.
//
// Tập con gồm object/array/string/integer/number/boolean/null, required, enum và
// additionalProperties. Khi không khai báo additionalProperties thì theo ngữ nghĩa
// mặc định của JSON Schema, không từ chối trường lạ.
//
// Thông báo lỗi ở đây được đưa lại cho model để tự sửa, nên văn bản theo ngôn
// ngữ tác phẩm ở cấp tiến trình (i18n.Active()).
func ValidateJSON(schema map[string]any, raw []byte) error {
	return validateJSONWith(schema, raw, contractTextsFor(i18n.Active()))
}

// validateJSONWith là biến thể có truyền sẵn bảng văn bản, dùng khi lời gọi
// đã biết ngôn ngữ của hợp đồng (xem Execute).
func validateJSONWith(schema map[string]any, raw []byte, texts *contractTexts) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf(texts.jsonParseFailed, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New(texts.jsonExtraValue)
		}
		return fmt.Errorf(texts.jsonTrailingInvalid, err)
	}
	return validateValue(schema, value, "$", texts)
}

func validateValue(schema map[string]any, value any, path string, texts *contractTexts) error {
	types, err := schemaTypes(schema["type"], texts)
	if err != nil {
		return fmt.Errorf(texts.contractInvalid, path, err)
	}
	if value == nil && !slices.Contains(types, "null") {
		return fmt.Errorf(texts.mustBeType, path, joinTypes(types, texts), "null")
	} else if value != nil {
		actual := valueType(value)
		if len(types) > 0 && !slices.Contains(types, actual) && !(actual == "integer" && slices.Contains(types, "number")) {
			return fmt.Errorf(texts.mustBeType, path, joinTypes(types, texts), actual)
		}
	}

	if rawEnum, exists := schema["enum"]; exists {
		enum, err := enumValues(rawEnum, texts)
		if err != nil {
			return fmt.Errorf(texts.contractInvalid, path+".enum", err)
		}
		if !enumContains(enum, value) {
			return fmt.Errorf(texts.mustBeOneOf, path, enum, value)
		}
	}
	if value == nil {
		return nil
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return fmt.Errorf(texts.missingProperties, path)
		}
		required, err := requiredNames(schema["required"], texts)
		if err != nil {
			return fmt.Errorf(texts.requiredInvalid, path, err)
		}
		for _, name := range required {
			if _, exists := typed[name]; !exists {
				return fmt.Errorf(texts.requiredField, path, name)
			}
		}
		if allowAdditional, declared := schema["additionalProperties"].(bool); declared && !allowAdditional {
			for name := range typed {
				if _, exists := properties[name]; !exists {
					return fmt.Errorf(texts.undeclaredField, path, name)
				}
			}
		}
		for name, child := range typed {
			childSchema, exists := properties[name]
			if !exists {
				continue
			}
			childMap, ok := childSchema.(map[string]any)
			if !ok {
				return fmt.Errorf(texts.notAnObject, path, name)
			}
			if err := validateValue(childMap, child, path+"."+name, texts); err != nil {
				return err
			}
		}
	case []any:
		itemSchema, ok := schema["items"].(map[string]any)
		if !ok {
			return fmt.Errorf(texts.missingItems, path)
		}
		for i, item := range typed {
			if err := validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), texts); err != nil {
				return err
			}
		}
	}
	return nil
}

func schemaTypes(value any, texts *contractTexts) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{typed}, nil
	case []string:
		return typed, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New(texts.unionOnlyStrings)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, errors.New(texts.typeMustBe)
	}
}

func requiredNames(value any, texts *contractTexts) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if names, ok := stringSlice(value); ok {
		return names, nil
	}
	return nil, errors.New(texts.mustBeStringArray)
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func enumValues(value any, texts *contractTexts) ([]any, error) {
	switch typed := value.(type) {
	case []string:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = item
		}
		return out, nil
	case []any:
		for _, item := range typed {
			if item == nil {
				continue
			}
			if _, ok := item.(string); !ok {
				return nil, errors.New(texts.onlyStringOrNull)
			}
		}
		return typed, nil
	default:
		return nil, errors.New(texts.mustBeArray)
	}
}

func enumContains(enum []any, value any) bool {
	for _, item := range enum {
		if item == nil && value == nil {
			return true
		}
		itemText, itemOK := item.(string)
		valueText, valueOK := value.(string)
		if itemOK && valueOK && itemText == valueText {
			return true
		}
	}
	return false
}

func valueType(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if number, err := strconv.ParseFloat(string(typed), 64); err == nil && !math.IsInf(number, 0) && math.Trunc(number) == number {
			return "integer"
		}
		return "number"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func joinTypes(types []string, texts *contractTexts) string {
	if len(types) == 0 {
		return texts.validJSONValue
	}
	return fmt.Sprint(types)
}
