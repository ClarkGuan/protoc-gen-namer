package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	readAll, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalln(err)
	}
	req := new(pluginpb.CodeGeneratorRequest)
	if err := proto.Unmarshal(readAll, req); err != nil {
		log.Fatalln(err)
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: req.ProtoFile})
	if err != nil {
		log.Fatalln(err)
	}
	buf := new(strings.Builder)
	writer := io.MultiWriter(buf, os.Stderr)
	for _, file := range req.ProtoFile {
		fileDescriptor, err := files.FindFileByPath(file.GetName())
		if err != nil {
			log.Fatalln(err)
		}
		messages := fileDescriptor.Messages()
		for i := 0; i < messages.Len(); i++ {
			message := messages.Get(i)
			_, _ = fmt.Fprintln(writer, string(message.FullName()), fullNameOfMessage(message))
			oneofs := message.Oneofs()
			for j := 0; j < oneofs.Len(); j++ {
				oneof := oneofs.Get(j)
				_, _ = fmt.Fprintln(writer, string(oneof.FullName()), fullNameOfOneof(oneof))
			}
		}
		enums := fileDescriptor.Enums()
		for i := 0; i < enums.Len(); i++ {
			enum := enums.Get(i)
			_, _ = fmt.Fprintln(writer, string(enum.FullName()), fullNameOfEnum(enum))
		}
	}
	resp := pluginpb.CodeGeneratorResponse{File: []*pluginpb.CodeGeneratorResponse_File{
		{Name: proto.String("mapper.txt"), Content: proto.String(buf.String())}}}
	content, err := proto.Marshal(&resp)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = os.Stdout.Write(content)
	if err != nil {
		log.Fatalln(err)
	}
}

func fullNameOfMessage(message protoreflect.MessageDescriptor) string {
	relativeName := relativeNameOfMessage(message)
	if container, ok := message.Parent().(protoreflect.MessageDescriptor); ok {
		return fullNameOfMessage(container) + "." + relativeName
	} else {
		return relativeName
	}
}

func relativeNameOfMessage(message protoreflect.MessageDescriptor) string {
	if _, ok := message.Parent().(protoreflect.MessageDescriptor); ok {
		return sanitizeMessage(string(message.Name()))
	} else {
		prefix := typePrefix(message.ParentFile())
		return sanitizeMessage(prefix + string(message.Name()))
	}
}

func fullNameOfEnum(enum protoreflect.EnumDescriptor) string {
	relativeName := relativeNameOfEnum(enum)
	if container, ok := enum.Parent().(protoreflect.MessageDescriptor); ok {
		return fullNameOfMessage(container) + "." + relativeName
	} else {
		return relativeName
	}
}

func relativeNameOfEnum(enum protoreflect.EnumDescriptor) string {
	if _, ok := enum.Parent().(protoreflect.MessageDescriptor); ok {
		return sanitizeEnum(string(enum.Name()))
	} else {
		prefix := typePrefix(enum.ParentFile())
		return sanitizeMessage(prefix + string(enum.Name()))
	}
}

func fullNameOfOneof(oneof protoreflect.OneofDescriptor) string {
	return fullNameOfMessage(oneof.Parent().(protoreflect.MessageDescriptor)) + "." + relativeNameOfOneof(oneof)
}

func relativeNameOfOneof(oneof protoreflect.OneofDescriptor) string {
	camelCase := toUpperCamelCase(string(oneof.Name()))
	return sanitizeOneof("OneOf_" + camelCase)
}

func sanitizeMessage(name string) string {
	return sanitizeTypeName(name, "Message")
}

func sanitizeEnum(name string) string {
	return sanitizeTypeName(name, "Enum")
}

func sanitizeOneof(name string) string {
	return sanitizeTypeName(name, "Oneof")
}

func sanitizeTypeName(name, disambiguator string) string {
	if _, ok := reservedNames[name]; ok {
		return name + disambiguator
	} else if isAllUnderscore(name) {
		return name + disambiguator
	} else if strings.HasSuffix(name, disambiguator) {
		return sanitizeTypeName(name[:len(name)-len(disambiguator)], disambiguator) + disambiguator
	}
	return name
}

func isAllUnderscore(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, s := range name {
		if s != '_' {
			return false
		}
	}
	return true
}

func typePrefix(file protoreflect.FileDescriptor) string {
	return typePrefixInternal(string(file.Package()), file.Options().(*descriptorpb.FileOptions))
}

func typePrefixInternal(packageName string, options *descriptorpb.FileOptions) string {
	swiftPrefix := options.GetSwiftPrefix()
	if len(swiftPrefix) > 0 {
		return swiftPrefix
	}
	if len(packageName) == 0 {
		return ""
	}
	ret := make([]rune, 0, len(packageName)+1)
	makeUpper := true
	for _, c := range packageName {
		if c == '_' {
			makeUpper = true
		} else if c == '.' {
			makeUpper = true
			ret = append(ret, '_')
		} else {
			if len(ret) == 0 && unicode.IsNumber(c) {
				ret = append(ret, '_')
			}
			if makeUpper {
				ret = append(ret, unicode.ToUpper(c))
				makeUpper = false
			} else {
				ret = append(ret, c)
			}
		}
	}
	ret = append(ret, '_')
	return string(ret)
}

func toUpperCamelCase(name string) string {
	return transform(name, true)
}

var appreviations = map[string]bool{
	"url":   true,
	"http":  true,
	"https": true,
	"id":    true,
}

type CharKind int

const (
	digit CharKind = iota
	lower
	upper
	underscore
	other
)

func toCharKind(c rune) CharKind {
	switch {
	case c >= '0' && c <= '9':
		return digit
	case c >= 'a' && c <= 'z':
		return lower
	case c >= 'A' && c <= 'Z':
		return upper
	case c == '_':
		return underscore
	default:
		return other
	}
}

func transform(name string, initialUpperCase bool) string {
	result := new(strings.Builder)
	var current []rune
	lastKind := other

	addCurrent := func() {
		if len(current) == 0 {
			return
		}
		currentAsString := string(current)
		if result.Len() == 0 && !initialUpperCase {
			// Nothing, want it to stay lowercase.
		} else if _, ok := appreviations[currentAsString]; ok {
			currentAsString = strings.ToUpper(currentAsString)
		} else {
			currentAsString = uppercaseFirstCharacter(currentAsString)
		}
		result.WriteString(currentAsString)
		current = current[:0]
	}

	for _, c := range name {
		kind := toCharKind(c)
		switch kind {
		case digit:
			if lastKind != digit {
				addCurrent()
			}
			if result.Len() == 0 {
				result.WriteRune('_')
			}
			current = append(current, c)

		case upper:
			if lastKind != upper {
				addCurrent()
			}
			current = append(current, unicode.ToLower(c))

		case lower:
			if lastKind != lower && lastKind != upper {
				addCurrent()
			}
			current = append(current, c)

		case underscore:
			addCurrent()
			if lastKind == underscore {
				result.WriteRune('_')
			}

		case other:
			addCurrent()
			escapeIt := false
			if result.Len() == 0 {
				escapeIt = !isSwiftIdentifierHeadCharacter(c)
			} else {
				escapeIt = !isSwiftIdentifierCharacter(c)
			}
			if escapeIt {
				_, _ = fmt.Fprintf(result, "_u%d", c)
			} else {
				current = append(current, c)
			}

		default:
			panic("can't reach here")
		}

		lastKind = kind
	}
	// Add the last segment collected.
	addCurrent()

	// If things end in an underscore, add one also.
	if lastKind == underscore {
		result.WriteRune('_')
	}

	return result.String()
}

func uppercaseFirstCharacter(s string) string {
	if len(s) == 0 {
		return s
	}
	ret := []rune(s)
	ret[0] = unicode.ToUpper(ret[0])
	return string(ret)
}

func isSwiftIdentifierHeadCharacter(c rune) bool {
	switch {
	case (c >= 0x61 && c <= 0x7a) || (c >= 0x41 && c <= 0x5a):
		fallthrough
	case c == 0x5f:
		fallthrough
	case (c == 0xa8) || (c == 0xaa) || (c == 0xad) || (c == 0xaf) || (c >= 0xb2 && c <= 0xb5) || (c >= 0xb7 && c <= 0xba):
		fallthrough
	case (c >= 0xbc && c <= 0xbe) || (c >= 0xc0 && c <= 0xd6) || (c >= 0xd8 && c <= 0xf6) || (c >= 0xf8 && c <= 0xff):
		fallthrough
	case (c >= 0x100 && c <= 0x2ff) || (c >= 0x370 && c <= 0x167f) || (c >= 0x1681 && c <= 0x180d) || (c >= 0x180f && c <= 0x1dbf):
		fallthrough
	case c >= 0x1e00 && c <= 0x1fff:
		fallthrough
	case (c >= 0x200b && c <= 0x200d) || (c >= 0x202a && c <= 0x202e) || (c == 0x203F) || (c == 0x2040) || (c == 0x2054) || (c >= 0x2060 && c <= 0x206f):
		fallthrough
	case (c >= 0x2070 && c <= 0x20cf) || (c >= 0x2100 && c <= 0x218f) || (c >= 0x2460 && c <= 0x24ff) || (c >= 0x2776 && c <= 0x2793):
		fallthrough
	case (c >= 0x2c00 && c <= 0x2dff) || (c >= 0x2e80 && c <= 0x2fff):
		fallthrough
	case (c >= 0x3004 && c <= 0x3007) || (c >= 0x3021 && c <= 0x302f) || (c >= 0x3031 && c <= 0x303f) || (c >= 0x3040 && c <= 0xd7ff):
		fallthrough
	case (c >= 0xf900 && c <= 0xfd3d) || (c >= 0xfd40 && c <= 0xfdcf) || (c >= 0xfdf0 && c <= 0xfe1f) || (c >= 0xfe30 && c <= 0xfe44):
		fallthrough
	case c >= 0xfe47 && c <= 0xfffd:
		fallthrough
	case (c >= 0x10000 && c <= 0x1fffd) || (c >= 0x20000 && c <= 0x2fffd) || (c >= 0x30000 && c <= 0x3fffd) || (c >= 0x40000 && c <= 0x4fffd):
		fallthrough
	case (c >= 0x50000 && c <= 0x5fffd) || (c >= 0x60000 && c <= 0x6fffd) || (c >= 0x70000 && c <= 0x7fffd) || (c >= 0x80000 && c <= 0x8fffd):
		fallthrough
	case (c >= 0x90000 && c <= 0x9fffd) || (c >= 0xa0000 && c <= 0xafffd) || (c >= 0xb0000 && c <= 0xbfffd) || (c >= 0xc0000 && c <= 0xcfffd):
		fallthrough
	case (c >= 0xd0000 && c <= 0xdfffd) || (c >= 0xe0000 && c <= 0xefffd):
		return true

	default:
		return false
	}
}

func isSwiftIdentifierCharacter(c rune) bool {
	switch {
	case c >= 0x30 && c <= 0x39:
		fallthrough
	case (c >= 0x300 && c <= 0x36F) || (c >= 0x1dc0 && c <= 0x1dff) || (c >= 0x20d0 && c <= 0x20ff) || (c >= 0xfe20 && c <= 0xfe2f):
		return true

	default:
		return isSwiftIdentifierHeadCharacter(c)
	}
}

var reservedNames = map[string]bool{
	"SwiftProtobuf":    true,
	"Extensions":       true,
	"protoMessageName": true,
	"decodeMessage":    true,
	"traverse":         true,
	"isInitialized":    true,
	"unknownFields":    true,
	"debugDescription": true,
	"description":      true,
	"dynamicType":      true,
	"hashValue":        true,
	"Type":             true,
	"Protocol":         true,
}

var swiftKeywordsUsedInDeclarations = []string{
	"associatedtype", "class", "deinit", "enum", "extension",
	"fileprivate", "func", "import", "init", "inout", "internal",
	"let", "open", "operator", "private", "protocol", "public",
	"static", "struct", "subscript", "typealias", "var",
}

var swiftKeywordsUsedInStatements = []string{
	"break", "case",
	"continue", "default", "defer", "do", "else", "fallthrough",
	"for", "guard", "if", "in", "repeat", "return", "switch", "where",
	"while",
}

var swiftKeywordsUsedInExpressionsAndTypes = []string{
	"as",
	"Any", "catch", "false", "is", "nil", "rethrows", "super", "self",
	"Self", "throw", "throws", "true", "try",
}

var swiftCommonTypes = []string{
	"Bool", "Data", "Double", "Float", "Int",
	"Int32", "Int64", "String", "UInt", "UInt32", "UInt64",
}

var swiftSpecialVariables = []string{
	"__COLUMN__",
	"__FILE__", "__FUNCTION__", "__LINE__",
}

func init() {
	for _, keyword := range swiftKeywordsUsedInDeclarations {
		reservedNames[keyword] = true
	}
	for _, keyword := range swiftKeywordsUsedInStatements {
		reservedNames[keyword] = true
	}
	for _, keyword := range swiftKeywordsUsedInExpressionsAndTypes {
		reservedNames[keyword] = true
	}
	for _, commonType := range swiftCommonTypes {
		reservedNames[commonType] = true
	}
	for _, variable := range swiftSpecialVariables {
		reservedNames[variable] = true
	}
}
