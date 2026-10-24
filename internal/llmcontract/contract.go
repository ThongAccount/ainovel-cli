// Package llmcontract là lớp hợp đồng và thực thi chung cho các lần trả về
// có cấu trúc trực tiếp: Contract tĩnh là nguồn duy nhất của cấu trúc, Execute
// gộp chọn khả năng, chuẩn bị prompt, thử lại yêu cầu, giải mã Schema/DTO và
// tự lành theo phản hồi.
//
// Giao thức được chốt trước khi gửi yêu cầu; yêu cầu gốc bị từ chối hoặc vi
// phạm hợp đồng thì phải phơi bày nguyên trạng, cấm lặng lẽ bỏ schema đi gửi lại.
package llmcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Contract là hợp đồng tĩnh cho một lần trả về có cấu trúc trực tiếp, đặt sát
// DTO biên giới của từng nơi dùng.
//
// Lang chọn ngôn ngữ cho văn bản hợp đồng (hậu tố prompt và câu nhắc tự sửa).
// Rỗng → dùng i18n.Active(), tức ngôn ngữ tác phẩm ở cấp tiến trình.
type Contract struct {
	Name        string
	Description string
	Schema      map[string]any
	Lang        i18n.Lang
}

// lang trả về ngôn ngữ hiệu lực của hợp đồng: Lang nếu có, nếu không thì
// ngôn ngữ tác phẩm ở cấp tiến trình.
func (c Contract) lang() i18n.Lang {
	if c.Lang != "" {
		return c.Lang
	}
	return i18n.Active()
}

// Mode là giao thức có cấu trúc được dùng cho lần gọi này.
type Mode string

const (
	ModeNativeJSONSchema Mode = "native_json_schema"
	ModePromptContract   Mode = "prompt_contract"
)

// Source là nguồn của căn cứ chọn khả năng.
type Source string

const (
	SourceConfig  Source = "config"  // người dùng khai báo tường minh trong ModelConfig.json_schema
	SourceAdapter Source = "adapter" // bảng khả năng theo mô hình của provider adapter
	SourceUnknown Source = "unknown" // không có khai báo và khả năng chưa biết, đi theo prompt contract
)

// Resolution là kết quả chọn giao thức trước khi gửi yêu cầu, để nơi gọi
// rẽ nhánh và ghi nhật ký.
type Resolution struct {
	Mode     Mode
	Source   Source
	Strict   bool // có mang strict hay không ở chế độ native
	Provider string
	Model    string
}

// jsonSchemaOverrider do lớp bọc mô hình mang trạng thái ba giá trị từ cấu hình
// hiện thực (bootstrap.SwappableModel và các lớp bọc xuyên qua nó).
type jsonSchemaOverrider interface {
	JSONSchemaOverride() *bool
}

type modelInfoProvider interface {
	Info() llm.ModelInfo
}

// ModelFacts là ảnh chụp cùng một thời điểm cho một lần phân giải khả năng.
// Lớp bọc hot-swap hiện thực giao diện này, nhờ vậy Resolve không đọc khả năng,
// ghi đè cấu hình và danh tính mô hình ở ba thời điểm khác nhau — tránh trộn
// trạng thái của hai lần chuyển mô hình.
type ModelFacts struct {
	Capabilities       llm.Capabilities
	Info               llm.ModelInfo
	JSONSchemaOverride *bool
}

type modelFactsProvider interface {
	StructuredOutputFacts() ModelFacts
}

// Resolve đọc trạng thái mô hình hiện tại ở mỗi lần gọi (sau hot-swap thì lần
// gọi kế tiếp dùng giá trị mới): cấu hình ba giá trị ưu tiên, sau đó là khả
// năng theo mô hình của adapter, không rõ thì luôn đi theo prompt contract.
func Resolve(model any) Resolution {
	res := Resolution{Mode: ModePromptContract, Source: SourceUnknown}

	var caps llm.Capabilities
	var info llm.ModelInfo
	var override *bool
	if fp, ok := model.(modelFactsProvider); ok {
		facts := fp.StructuredOutputFacts()
		caps, info, override = facts.Capabilities, facts.Info, facts.JSONSchemaOverride
	} else {
		if cp, ok := model.(llm.CapabilityProvider); ok {
			caps = cp.Capabilities()
		}
		if ip, ok := model.(modelInfoProvider); ok {
			info = ip.Info()
		}
		if o, ok := model.(jsonSchemaOverrider); ok {
			override = o.JSONSchemaOverride()
		}
	}
	res.Provider, res.Model = caps.Provider, caps.Model
	if res.Provider == "" {
		res.Provider = info.Provider
	}
	if res.Model == "" {
		res.Model = info.Name
	}

	if override != nil {
		res.Source = SourceConfig
		if *override {
			res.Mode = ModeNativeJSONSchema
			// Người dùng khai báo endpoint tuân thủ hợp đồng Structured Outputs
			// thì mặc định mang strict; chỉ khi adapter nói rõ không hỗ trợ
			// strict mới gửi schema mà không gửi strict.
			res.Strict = caps.Structured.Strict != llm.SupportNo
		}
		return res
	}

	switch caps.Structured.JSONSchema {
	case llm.SupportYes:
		res.Mode = ModeNativeJSONSchema
		res.Source = SourceAdapter
		res.Strict = caps.Structured.Strict == llm.SupportYes
	case llm.SupportNo:
		res.Source = SourceAdapter
	}
	return res
}

// Plan phân giải giao thức và sinh tuỳ chọn gọi ở chế độ native; ở chế độ
// prompt contract trả về nil opts.
func Plan(model any, c Contract) ([]agentcore.CallOption, Resolution) {
	res := Resolve(model)
	if res.Mode != ModeNativeJSONSchema {
		return nil, res
	}
	return []agentcore.CallOption{
		agentcore.WithJSONSchema(c.Name, c.Description, c.Schema, res.Strict),
	}, res
}

// PreparePrompt giữ đúng một bản prompt ngữ nghĩa nghiệp vụ: chế độ native trả
// về nguyên văn; chế độ prompt contract tự sinh hậu tố định dạng từ chính
// Schema đó. Nơi gọi không phải giữ một bộ mẫu thứ hai, nên đổi trường cũng
// không làm prompt và response_format lệch nhau.
//
// Văn bản hợp đồng theo ngôn ngữ của Contract.Lang, rỗng thì theo ngôn ngữ
// tác phẩm ở cấp tiến trình.
func PreparePrompt(base string, c Contract, res Resolution) (string, error) {
	if res.Mode != ModePromptContract {
		return base, nil
	}
	schemaJSON, err := json.Marshal(c.Schema)
	if err != nil {
		return "", fmt.Errorf("llmcontract: marshal %s prompt schema: %w", c.Name, err)
	}
	contract := contractTextsFor(c.lang()).outputContract +
		"\n\n<output-json-schema>\n" + string(schemaJSON) + "\n</output-json-schema>"
	if strings.TrimSpace(base) == "" {
		return contract, nil
	}
	return strings.TrimSpace(base) + "\n\n" + contract, nil
}

// Nullable mở rộng type của một schema thành hợp nhất có null (["<t>","null"]),
// dùng để diễn đạt "tùy chọn" trong chế độ strict vốn bắt buộc mọi trường
// phải required. Trả về bản sao, không sửa map được truyền vào.
func Nullable(s map[string]any) map[string]any {
	out := maps.Clone(s)
	if t, ok := out["type"].(string); ok {
		out["type"] = []string{t, "null"}
	}
	switch values := out["enum"].(type) {
	case []string:
		enum := make([]any, 0, len(values)+1)
		for _, value := range values {
			enum = append(enum, value)
		}
		out["enum"] = append(enum, nil)
	case []any:
		enum := slices.Clone(values)
		for _, value := range enum {
			if value == nil {
				return out
			}
		}
		out["enum"] = append(enum, nil)
	}
	return out
}

// ValidateStrictReady kiểm tra đệ quy xem schema có thoả điều kiện cấu trúc của
// tập con strict trong OpenAI hay không: mọi thuộc tính của object đều phải
// nằm trong required (ngữ nghĩa "tùy chọn" được diễn đạt bằng hợp nhất null).
// litellm kiểm tra sẵn lúc gửi yêu cầu và tự thêm additionalProperties:false;
// hợp đồng kiểm thử dùng hàm này để tiên kiểm (RFC §11.1), không để lỗi cấu
// trúc lọt tới runtime.
func ValidateStrictReady(s map[string]any) error {
	return validateStrictReady(s, "$")
}

func validateStrictReady(s map[string]any, path string) error {
	texts := contractTextsFor(i18n.Active())
	if typeIncludes(s["type"], "object") {
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]string)
		for name, sub := range props {
			if !slices.Contains(required, name) {
				return fmt.Errorf(texts.strictNotRequired, path, name)
			}
			if subMap, ok := sub.(map[string]any); ok {
				if err := validateStrictReady(subMap, path+"."+name); err != nil {
					return err
				}
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		return validateStrictReady(items, path+"[]")
	}
	return nil
}

func typeIncludes(t any, want string) bool {
	switch v := t.(type) {
	case string:
		return v == want
	case []string:
		return slices.Contains(v, want)
	}
	return false
}

// Fingerprint trả về 12 ký tự hex đầu của sha256 của JSON chuẩn hóa schema,
// dùng để đối chiếu nhật ký; encoding/json sắp xếp khoá map nên cùng một
// hợp đồng luôn cho cùng một vân tay.
func (c Contract) Fingerprint() string {
	data, err := json.Marshal(c.Schema)
	if err != nil {
		return "unmarshalable"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}
