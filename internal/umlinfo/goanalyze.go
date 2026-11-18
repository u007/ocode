package umlinfo

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
)

// analyzeGoFile parses one Go source file and returns the navigable
// symbols it declares plus the import paths it uses. Root and rel are
// used to anchor positions and IDs: root is the index root, rel the
// slash-separated path relative to it.
func analyzeGoFile(root, rel string, data []byte) ([]Symbol, []string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), data,
		parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	isTest := strings.HasSuffix(rel, "_test.go")
	src := func(node ast.Node) string {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fset, node); err != nil {
			return ""
		}
		return b.String()
	}
	lineOf := func(pos token.Pos) int { return fset.Position(pos).Line }
	posOf := func(node ast.Node) (int, int) {
		return lineOf(node.Pos()), fset.Position(node.End()).Line
	}
	docOf := func(cg *ast.CommentGroup) string { return firstDocParagraph(cg) }

	var imports []string
	for _, imp := range f.Imports {
		if imp.Path != nil {
			p := strings.Trim(imp.Path.Value, `"`)
			imports = append(imports, p)
		}
	}

	var syms []Symbol
	mkID := func(name string, line int) string {
		return fmt.Sprintf("%s:%d:%s", rel, line, sanitizeID(name))
	}

	addType := func(ts *ast.TypeSpec, doc *ast.CommentGroup, isAlias bool) {
		line, endLine := posOf(ts)
		name := ts.Name.Name
		s := Symbol{
			ID:      mkID(name, line),
			Name:    name,
			Line:    line,
			EndLine: endLine,
			Doc:     docOf(doc),
			Lang:    "Go",
			File:    rel,
			IsTest:  isTest,
		}
		switch t := ts.Type.(type) {
		case *ast.StructType:
			s.Kind = "struct"
			if t.Fields != nil {
				for _, fld := range t.Fields.List {
					for _, n := range fld.Names {
						if len(s.Members) >= MaxMembersPerSymbol {
							break
						}
						s.Members = append(s.Members, Member{Kind: "field", Name: n.Name, Sig: src(fld.Type)})
					}
					if fld.Names == nil && len(s.Members) < MaxMembersPerSymbol {
						// Embedded field.
						s.Members = append(s.Members, Member{Kind: "field", Name: "(embedded) " + src(fld.Type), Sig: src(fld.Type)})
					}
				}
			}
		case *ast.InterfaceType:
			s.Kind = "interface"
			if t.Methods != nil {
				for _, m := range t.Methods.List {
					if len(s.Members) >= MaxMembersPerSymbol {
						break
					}
					if len(m.Names) == 0 {
						s.Members = append(s.Members, Member{Kind: "field", Name: "(embedded) " + src(m.Type), Sig: src(m.Type)})
						continue
					}
					s.Members = append(s.Members, Member{Kind: "method", Name: m.Names[0].Name, Sig: src(m.Type)})
				}
			}
		default:
			if isAlias {
				s.Kind = "type"
			} else {
				s.Kind = "type"
			}
			s.Sig = src(ts.Type)
		}
		syms = append(syms, s)
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// Prefer the spec's own doc (grouped const-style decl);
				// fall back to the GenDecl doc for single-type decls.
				doc := ts.Doc
				if doc == nil {
					doc = d.Doc
				}
				addType(ts, doc, d.Tok == token.TYPE && ts.Assign.IsValid())
			}
		case *ast.FuncDecl:
			line, endLine := posOf(d)
			name := d.Name.Name
			s := Symbol{
				Name:    name,
				Line:    line,
				EndLine: endLine,
				Doc:     docOf(d.Doc),
				Sig:     funcSignature(d, src),
				Lang:    "Go",
				File:    rel,
				IsTest:  isTest,
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind = "method"
				s.Receiver = receiverTypeName(d.Recv.List[0].Type)
			} else {
				s.Kind = "func"
			}
			s.ID = mkID(s.Name, line)
			if s.Receiver != "" {
				s.ID = mkID(s.Receiver+"."+s.Name, line)
			}
			syms = append(syms, s)
		}
	}
	return syms, imports, nil
}

// firstDocParagraph returns the first paragraph of a GoDoc comment group,
// trimmed, capped at MaxDocLen.
func firstDocParagraph(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	text := cg.Text()
	if i := strings.Index(text, "\n\n"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	if len(text) > MaxDocLen {
		text = strings.TrimSpace(text[:MaxDocLen]) + "…"
	}
	return text
}

// funcSignature renders "(receiver) name(params) (results)" without the body.
func funcSignature(d *ast.FuncDecl, src func(ast.Node) string) string {
	fn := *d
	fn.Body = nil
	fn.Doc = nil
	return strings.Join(strings.Fields(src(&fn)), " ")
}

// receiverTypeName unwraps *, parens and generics to the bare type name.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.ParenExpr:
		return receiverTypeName(t.X)
	case *ast.IndexExpr: // generic receiver: T[P]
		return receiverTypeName(t.X)
	case *ast.IndexListExpr: // generic receiver: T[P, Q]
		return receiverTypeName(t.X)
	default:
		return ""
	}
}

// sanitizeID keeps symbol IDs URL/JSON-friendly.
func sanitizeID(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}
