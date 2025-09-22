package docx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// Translator 结构体，用于配置翻译 API
type Translator struct {
	APIKey string
	APIURL string
	Client *http.Client
}

// NewTranslator 创建一个新的 Translator 实例
func NewTranslator(apiKey, apiURL string) *Translator {
	return &Translator{
		APIKey: apiKey,
		APIURL: apiURL,
		Client: &http.Client{},
	}
}

// Translate 使用 OpenAI 兼容的 API 翻译文本
func (t *Translator) Translate(text, targetLanguage string) (string, error) {
	if text == "" {
		return "", nil
	}

	reqBody := map[string]interface{}{
		"model": "gpt-3.5-turbo", // 您可以使用任何兼容的模型
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a professional translator.",
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("Translate the following text to %s: %s", targetLanguage, text),
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", t.APIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := t.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("invalid API response format: no choices found")
	}

	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid API response format: invalid choice format")
	}

	message, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid API response format: no message found")
	}

	translatedText, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid API response format: no content found in message")
	}

	return translatedText, nil
}

// TranslateDocx 翻译一个 docx 对象，并返回一个新的翻译后的 docx 对象
// TranslateDocx 翻译一个 docx 对象，并返回一个新的翻译后的 docx 对象 (优化版)
func (t *Translator) TranslateDocx(doc *Docx, targetLanguage string) (*Docx, error) {
	newDoc := New().WithDefaultTheme().WithA4Page()
	newDoc.media = doc.media
	newDoc.mediaNameIdx = doc.mediaNameIdx

	// 辅助函数，用于翻译段落内容
	translateParagraphContent := func(p *Paragraph) (*Paragraph, error) {
		// 1. 拼接整个段落的文本
		var textToTranslateBuilder strings.Builder
		for _, child := range p.Children {
			if run, ok := child.(*Run); ok {
				for _, grandChild := range run.Children {
					if text, ok := grandChild.(*Text); ok {
						textToTranslateBuilder.WriteString(text.Text)
					}
				}
			}
		}
		textToTranslate := textToTranslateBuilder.String()

		// 2. 如果段落有实际内容，则进行翻译
		if strings.TrimSpace(textToTranslate) == "" {
			// 对于空段落或只有空格的段落，直接复制
			return p, nil
		}

		translatedText, err := t.TranslateWithDashscope(textToTranslate, targetLanguage)
		if err != nil {
			// 如果翻译出错，则保留原文并打印错误
			fmt.Printf("翻译段落时出错: %v. 将保留原文.\n", err)
			translatedText = textToTranslate
		}

		// 3. 将翻译结果放入新段落，并尽量保留格式
		newPara := &Paragraph{
			Properties: p.Properties,
			Children:   make([]interface{}, 0),
			file:       newDoc,
		}

		if len(p.Children) > 0 {
			// 创建一个新的 Run 来存放完整的翻译文本
			// 并继承原段落第一个 Run 的格式
			newRun := &Run{
				RunProperties: &RunProperties{},
				Children:      []interface{}{&Text{Text: translatedText}},
			}

			if firstRun, ok := p.Children[0].(*Run); ok {
				newRun.RunProperties = firstRun.RunProperties
			}
			newPara.Children = append(newPara.Children, newRun)
		}

		return newPara, nil
	}

	// --- 遍历文档主体 ---
	for _, item := range doc.Document.Body.Items {
		switch o := item.(type) {
		case *Paragraph:
			translatedPara, _ := translateParagraphContent(o)
			newDoc.Document.Body.Items = append(newDoc.Document.Body.Items, translatedPara)

		case *Table:
			// 创建结构相同的新表格
			newTable := newDoc.AddTable(len(o.TableRows), len(o.TableRows[0].TableCells), 0, nil)
			newTable.TableProperties = o.TableProperties
			newTable.TableGrid = o.TableGrid

			// 遍历并翻译表格中的每一个单元格
			for i, row := range o.TableRows {
				for j, cell := range row.TableCells {
					newCell := newTable.TableRows[i].TableCells[j]
					newCell.TableCellProperties = cell.TableCellProperties
					newCell.Paragraphs = make([]*Paragraph, 0) // 清空默认段落

					for _, para := range cell.Paragraphs {
						translatedPara, _ := translateParagraphContent(para)
						newCell.Paragraphs = append(newCell.Paragraphs, translatedPara)
					}
				}
			}
			newDoc.Document.Body.Items = append(newDoc.Document.Body.Items, newTable)
		}
	}
	return newDoc, nil
}

// --- 在 translator.go 文件中添加以下代码 ---

// Dashscope API 请求体结构
type DashscopeRequest struct {
	Model              string              `json:"model"`
	Messages           []map[string]string `json:"messages"`
	TranslationOptions map[string]string   `json:"translation_options"`
}

// TranslateWithDashscope 使用阿里云 Dashscope API 翻译文本
// sourceLang: 源语言代码 (例如 "auto", "zh", "en")
// targetLang: 目标语言代码 (例如 "English", "Chinese", "Japanese")
func (t *Translator) TranslateWithDashscope(text, targetLang string) (string, error) {
	if text == "" {
		return "", nil
	}

	// 构造符合 Dashscope API 格式的请求体
	reqBody := DashscopeRequest{
		Model: "qwen-plus",
		Messages: []map[string]string{
			{"role": "system", "content": "你是一个翻译大师，你需要将" + "中文" + "的用户输入内容翻译为:" + targetLang + ".注意 你只需要返回翻译后的内容，不要返回任何多余内容"},
			{"role": "user", "content": text},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("无法序列化请求体: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", t.APIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("无法创建 HTTP 请求: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	// 发送请求
	resp, err := t.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送 API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("API 请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析 JSON 响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("无法解析 API 响应: %w", err)
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("无效的 API 响应格式: 'choices' 字段不存在或为空")
	}

	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("无效的 API 响应格式: choice 格式错误")
	}

	message, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("无效的 API 响应格式: message 格式错误")
	}

	translatedText, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("无效的 API 响应格式: 未在 message 中找到 content")
	}
	fmt.Println(translatedText)
	return translatedText, nil
}
