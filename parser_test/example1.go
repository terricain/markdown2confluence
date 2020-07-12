package main

import (
	"fmt"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"io"
	"io/ioutil"
	"strings"
)

const MACRO_XML_START = `<ac:structured-macro ac:name="code">`
const MACRO_XML_LANGUAGE = `<ac:parameter ac:name="language">LANGUAGE</ac:parameter>`
const MACRO_XML_BODY = `<ac:plain-text-body><![CDATA[BODY]]></ac:plain-text-body>`
const MACRO_XML_STOP = `</ac:structured-macro>`


func renderHookDropCodeBlock(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	if _, ok := node.(*ast.CodeBlock); ok {
		codeBlock := node.(*ast.CodeBlock)
		parts := make([]string, 5)
		parts = append(parts, MACRO_XML_START)

		if len(codeBlock.Info) > 0 {
			parts = append(parts, strings.Replace(MACRO_XML_LANGUAGE, "LANGUAGE", string(codeBlock.Info), 1))
		}
		parts = append(parts, strings.Replace(MACRO_XML_BODY, "BODY", string(codeBlock.Literal), 1))
		parts = append(parts, MACRO_XML_STOP)

		_, _ = io.WriteString(w, strings.Join(parts, "\n"))

		return ast.GoToNext, true
	}

	return ast.GoToNext, false
}

func main() {
	data, _ := ioutil.ReadFile("test.md")

	opts := html.RendererOptions{
		Flags: html.CommonFlags,
		RenderNodeHook: renderHookDropCodeBlock,
	}
	renderer := html.NewRenderer(opts)
	htmlData := markdown.ToHTML(data, nil, renderer)

	fmt.Println(string(htmlData))
}
