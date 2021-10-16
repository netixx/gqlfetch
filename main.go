package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/vektah/gqlparser/ast"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed introspectionQuery.graphql
var introspectionQuery string

func main() {
	ctx := context.Background()
	endpoint := os.Getenv("SERVER_ENDPOINT")
	if strings.TrimSpace(endpoint) == "" {
		log.Fatal("SERVER_ENDPOINT must be provided")
	}

	authorization := os.Getenv("AUTHORIZATION_HEADER")

	buffer := new(bytes.Buffer)
	err := json.NewEncoder(buffer).Encode(graphQLRequest{Query: introspectionQuery})
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, buffer)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", authorization)
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req.WithContext(ctx))
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	var schemaResponse introspectionRes
	err = json.NewDecoder(res.Body).Decode(&schemaResponse)
	if err != nil {
		log.Fatal(err)
	}

	if len(schemaResponse.Errors) != 0 {
		log.Fatal(schemaResponse.Errors)
	}

	fmt.Println(printSchema(schemaResponse.Data.Schema))
}

type tabbedStringBuilder struct {
	sb          *strings.Builder
	IndentLevel int
}

func (sb *tabbedStringBuilder) WriteString(s string) {
	if sb.IndentLevel != 0 {
		sb.sb.WriteString(strings.Repeat("\t", sb.IndentLevel))
	}
	sb.sb.WriteString(s)
}

func (sb *tabbedStringBuilder) String() string {
	return sb.sb.String()
}

func printSchema(schema GraphQLSchema) string {
	sb := &tabbedStringBuilder{
		sb: &strings.Builder{},
	}

	printDirectives(sb, schema.Directives)
	sb.WriteString("\n")
	printTypes(sb, schema.Types)

	return sb.String()
}

func printDirectives(sb *tabbedStringBuilder, directives []Directive) {
	for _, directive := range directives {
		printDescription(sb, directive.Description)
		sb.WriteString(fmt.Sprintf("directive @%s", directive.Name))
		if len(directive.Args) > 0 {
			sb.WriteString("(\n")
			sb.IndentLevel += 1
			for _, arg := range directive.Args {
				printDescription(sb, arg.Description)
				sb.WriteString(fmt.Sprintf("%s: %s\n", arg.Name, arg.Type.String()))
			}
			sb.IndentLevel -= 1
			sb.WriteString(")")
		}

		sb.WriteString(" on ")
		for i, location := range directive.Locations {
			sb.WriteString(string(location))
			if i < len(directive.Locations)-1 {
				sb.WriteString(" | ")
			}
		}
		sb.WriteString("\n")
		sb.WriteString("\n")
	}
}

func printDescription(sb *tabbedStringBuilder, description string) {
	if description != "" {
		sb.WriteString(fmt.Sprintf(`"""%s"""`, description))
		sb.WriteString("\n")
	}
}

func printTypes(sb *tabbedStringBuilder, types []Types) {
	for _, typ := range types {
		printDescription(sb, typ.Description)

		switch typ.Kind {

		case ast.Object:
			sb.WriteString(fmt.Sprintf("type %s ", typ.Name))
			if len(typ.Interfaces) > 0 {
				sb.WriteString("implements ")
				for i, intface := range typ.Interfaces {
					sb.WriteString(intface.Name)
					if i < len(typ.Interfaces)-1 {
						sb.WriteString(" & ")
					}
				}
			}
			sb.WriteString("{\n")
			sb.IndentLevel += 1
			for _, field := range typ.Fields {
				printDescription(sb, field.Description)
				sb.WriteString(fmt.Sprintf("%s: %s\n", field.Name, field.Type.String()))
			}
			sb.IndentLevel -= 1
			sb.WriteString("}")

		case ast.Union:
			sb.WriteString(fmt.Sprintf("union %s =", typ.Name))
			var possible []*Type
			if err := json.Unmarshal(typ.PossibleTypes, &possible); err != nil {
				panic(err)
			}
			for i, typ := range possible {
				sb.WriteString(typ.String())
				if i < len(possible)-1 {
					sb.WriteString(" | ")
				}
			}

		case ast.Enum:
			sb.WriteString(fmt.Sprintf("enum %s {\n", typ.Name))
			var enumValues ast.EnumValueList
			if err := json.Unmarshal(typ.EnumValues, &enumValues); err != nil {
				panic(err)
			}
			sb.IndentLevel += 1
			for _, value := range enumValues {
				printDescription(sb, value.Description)
				sb.WriteString(fmt.Sprintf("%s\n", value.Name))
			}
			sb.IndentLevel -= 1
			sb.WriteString("}")

		case ast.Scalar:
			sb.WriteString(fmt.Sprintf("scalar %s", typ.Name))

		case ast.InputObject:
			sb.WriteString(fmt.Sprintf("input %s {\n", typ.Name))
			sb.IndentLevel += 1
			for _, field := range typ.Fields {
				printDescription(sb, typ.Description)
				sb.WriteString(fmt.Sprintf("%s: %s\n", field.Name, field.Type.String()))
			}
			sb.IndentLevel -= 1
			sb.WriteString("}")

		case ast.Interface:
			sb.WriteString(fmt.Sprintf("interface %s {\n", typ.Name))
			sb.IndentLevel += 1
			for _, field := range typ.Fields {
				printDescription(sb, typ.Description)
				sb.WriteString(fmt.Sprintf("%s: %s\n", field.Name, field.Type.String()))
			}
			sb.IndentLevel -= 1
			sb.WriteString("}")

		default:
			panic(fmt.Sprint("not handling", typ.Kind))
		}
		sb.WriteString("\n")
		sb.WriteString("\n")
	}
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type graphqlErrs []graphqlErr

type graphqlErr struct {
	Message string `json:"message"`
}

type introspectionRes struct {
	Errors graphqlErrs `json:"errors"`
	Data   struct {
		Schema GraphQLSchema `json:"__schema"`
	} `json:"data"`
}

type GraphQLSchema struct {
	QueryType    ast.Definition `json:"queryType"`
	MutationType ast.Definition `json:"mutationType"`
	Types        []Types        `json:"types"`
	Directives   []Directive    `json:"directives"`
}

type Types struct {
	Kind        ast.DefinitionKind `json:"kind"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Fields      []struct {
		Name              string        `json:"name"`
		Description       string        `json:"description"`
		Args              []interface{} `json:"args"`
		Type              *Type         `json:"type"`
		IsDeprecated      bool          `json:"isDeprecated"`
		DeprecationReason interface{}   `json:"deprecationReason"`
	} `json:"fields"`
	InputFields   []InputField     `json:"inputFields"`
	Interfaces    []ast.Definition `json:"interfaces"`
	EnumValues    json.RawMessage  `json:"enumValues"`
	PossibleTypes json.RawMessage  `json:"possibleTypes"`
}

type InputField struct {
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Type         Type        `json:"type"`
	DefaultValue interface{} `json:"defaultValue"`
}

type Directive struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Locations   []ast.DirectiveLocation `json:"locations"`
	Args        []struct {
		Name         string      `json:"name"`
		Description  string      `json:"description"`
		Type         *Type       `json:"type"`
		DefaultValue interface{} `json:"defaultValue"`
	} `json:"args"`
}

type Type struct {
	ast.Type
}

func (t *Type) UnmarshalJSON(data []byte) error {
	var typ introspectedType
	if err := json.Unmarshal(data, &typ); err != nil {
		return err
	}

	head := introspectionTypeToAstType(&typ)
	t.NamedType = head.NamedType
	t.Elem = head.Elem
	t.NonNull = head.NonNull

	return nil
}

func introspectionTypeToAstType(typ *introspectedType) *ast.Type {
	var res ast.Type
	if typ.OfType == nil {
		res.NamedType = *typ.Name
		return &res
	}

	switch typ.Kind {
	case NON_NULL:
		res.NonNull = true
		res.Elem = introspectionTypeToAstType(typ.OfType)
		return &res
	case LIST:
		res.Elem = introspectionTypeToAstType(typ.OfType)
		return &res
	}

	return nil
}

type introspectedType struct {
	Kind   TypeKind          `json:"kind"`
	Name   *string           `json:"name"`
	OfType *introspectedType `json:"ofType"`
}

type TypeKind string

const (
	NON_NULL TypeKind = "NON_NULL"
	LIST     TypeKind = "LIST"
)
