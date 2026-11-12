package yamldslspec

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	formatPartLimit       = 1000
	earliestFormatSeconds = -62135596800
	latestFormatSeconds   = 253402300800
	secondsPerDay         = 86400
	textOperandKinds      = "text"
	numberOperandKinds    = "a number"
	wholeOperandKinds     = "a whole number"
)

type formatVerbRule struct {
	acceptedKinds    string
	takesOperand     bool
	acceptsText      bool
	acceptsWhole     bool
	acceptsNumber    bool
	acceptsBoolean   bool
	readsWidth       bool
	readsPrecision   bool
	readsZeroPadding bool
	readsPlusSign    bool
}

var formatVerbRules = map[rune]formatVerbRule{
	'%': {},
	's': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'q': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'u': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'l': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'A': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'B': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'v': {
		acceptedKinds: "text, a whole number or a boolean", takesOperand: true,
		acceptsText: true, acceptsWhole: true, acceptsBoolean: true, readsWidth: true,
	},
	'd': {
		acceptedKinds: wholeOperandKinds, takesOperand: true,
		acceptsWhole: true, readsWidth: true, readsZeroPadding: true, readsPlusSign: true,
	},
	'x': {
		acceptedKinds: "a whole number or text", takesOperand: true,
		acceptsText: true, acceptsWhole: true, readsWidth: true, readsZeroPadding: true,
	},
	'X': {
		acceptedKinds: "a whole number or text", takesOperand: true,
		acceptsText: true, acceptsWhole: true, readsWidth: true, readsZeroPadding: true,
	},
	'f': {
		acceptedKinds: numberOperandKinds, takesOperand: true, acceptsWhole: true, acceptsNumber: true,
		readsWidth: true, readsPrecision: true, readsZeroPadding: true, readsPlusSign: true,
	},
	'C': {acceptedKinds: numberOperandKinds, takesOperand: true, acceptsWhole: true, acceptsNumber: true, readsWidth: true},
}

// LayoutPart is one part of a layout: its literal text, or a verb.
type LayoutPart struct {
	Text string
	Verb *LayoutVerb
}

// LayoutVerb is a verb as a layout writes it: its letter, its flags, and its
// width and precision, nil where left out.
type LayoutVerb struct {
	Letter      rune
	LeftAligned bool
	PlusSigned  bool
	ZeroPadded  bool
	Width       *int
	Precision   *int
}

type formatDirective struct {
	LayoutVerb
	writtenText   string
	overPartLimit bool
}

type formatOperandKind int

const (
	textOperand formatOperandKind = iota
	wholeOperand
	numberOperand
	booleanOperand
	otherOperand
)

type formatOperand struct {
	kind         formatOperandKind
	kindName     string
	textValue    string
	wholeValue   int64
	numberValue  float64
	booleanValue bool
}

// FormatLayout is the standard's format function: the layout with each verb
// written from the next operand, or the problem that stops it.
func FormatLayout(layout string, operands []any) (string, error) {
	layoutParts, layoutError := ParseLayout(layout)
	if layoutError != nil {
		return "", layoutError
	}
	var writtenText strings.Builder
	nextOperand := 0
	for _, layoutPart := range layoutParts {
		if layoutPart.Verb == nil {
			writtenText.WriteString(layoutPart.Text)
			continue
		}
		if nextOperand >= len(operands) {
			return "", fmt.Errorf("layout %s has more verbs than operands", CodeSpan(layout))
		}
		layoutVerb := *layoutPart.Verb
		verbRule := formatVerbRules[layoutVerb.Letter]
		operand := formatOperandOf(operands[nextOperand])
		nextOperand++
		verbText, verbError := formattedVerbText(layoutVerb, verbRule, operand, nextOperand)
		if verbError != nil {
			return "", verbError
		}
		writtenText.WriteString(paddedVerbText(layoutVerb, verbRule, verbText))
	}
	if nextOperand < len(operands) {
		return "", fmt.Errorf("layout %s has more operands than verbs", CodeSpan(layout))
	}
	return writtenText.String(), nil
}

// ParseLayout reads a layout into its parts, literal text and verbs, `%%`
// read as text; else the first problem in the layout.
func ParseLayout(layout string) ([]LayoutPart, error) {
	layoutParts := []LayoutPart{}
	var literalText strings.Builder
	layoutRunes := []rune(layout)
	for position := 0; position < len(layoutRunes); position++ {
		if layoutRunes[position] != '%' {
			literalText.WriteRune(layoutRunes[position])
			continue
		}
		directive, endPosition, isComplete := readFormatDirective(layoutRunes, position)
		if !isComplete {
			return nil, fmt.Errorf("layout %s ends inside a verb", CodeSpan(layout))
		}
		position = endPosition
		verbRule, isVerb := formatVerbRules[directive.Letter]
		if !isVerb {
			return nil, fmt.Errorf("%s is not a verb `format` writes", CodeSpan(directive.writtenText))
		}
		if directiveError := directivePartProblem(directive, verbRule); directiveError != nil {
			return nil, directiveError
		}
		if !verbRule.takesOperand {
			literalText.WriteRune('%')
			continue
		}
		if literalText.Len() > 0 {
			layoutParts = append(layoutParts, LayoutPart{Text: literalText.String()})
			literalText.Reset()
		}
		layoutVerb := directive.LayoutVerb
		layoutParts = append(layoutParts, LayoutPart{Verb: &layoutVerb})
	}
	if literalText.Len() > 0 {
		layoutParts = append(layoutParts, LayoutPart{Text: literalText.String()})
	}
	return layoutParts, nil
}

func readFormatDirective(layoutRunes []rune, startPosition int) (formatDirective, int, bool) {
	directive := formatDirective{}
	position := startPosition + 1
	for position < len(layoutRunes) && strings.ContainsRune("-+0", layoutRunes[position]) {
		switch layoutRunes[position] {
		case '-':
			directive.LeftAligned = true
		case '+':
			directive.PlusSigned = true
		default:
			directive.ZeroPadded = true
		}
		position++
	}
	directive.Width, position = readFormatNumber(layoutRunes, position, &directive)
	if position < len(layoutRunes) && layoutRunes[position] == '.' {
		directive.Precision, position = readFormatNumber(layoutRunes, position+1, &directive)
		if directive.Precision == nil {
			noPrecision := 0
			directive.Precision = &noPrecision
		}
	}
	if position >= len(layoutRunes) {
		return directive, position, false
	}
	directive.Letter = layoutRunes[position]
	directive.writtenText = string(layoutRunes[startPosition : position+1])
	return directive, position, true
}

func readFormatNumber(layoutRunes []rune, startPosition int, directive *formatDirective) (*int, int) {
	readNumber, position := 0, startPosition
	for position < len(layoutRunes) && layoutRunes[position] >= '0' && layoutRunes[position] <= '9' {
		if readNumber > formatPartLimit {
			directive.overPartLimit = true
		} else {
			readNumber = readNumber*10 + int(layoutRunes[position]-'0')
		}
		position++
	}
	if readNumber > formatPartLimit {
		directive.overPartLimit = true
	}
	if position == startPosition {
		return nil, position
	}
	return &readNumber, position
}

func directivePartProblem(directive formatDirective, verbRule formatVerbRule) error {
	fencedDirective := CodeSpan(directive.writtenText)
	switch {
	case directive.overPartLimit:
		return fmt.Errorf("%s has a width or a precision over %d", fencedDirective, formatPartLimit)
	case directive.LeftAligned && !verbRule.readsWidth:
		return fmt.Errorf("%s takes no `-` flag", fencedDirective)
	case directive.PlusSigned && !verbRule.readsPlusSign:
		return fmt.Errorf("%s takes no `+` flag", fencedDirective)
	case directive.ZeroPadded && !verbRule.readsZeroPadding:
		return fmt.Errorf("%s takes no `0` flag", fencedDirective)
	case directive.Width != nil && !verbRule.readsWidth:
		return fmt.Errorf("%s takes no width", fencedDirective)
	case directive.Precision != nil && !verbRule.readsPrecision:
		return fmt.Errorf("%s takes no precision", fencedDirective)
	case directive.Letter == 'B' && (directive.Precision == nil || *directive.Precision < 1):
		return fmt.Errorf("%s takes a precision of at least 1, such as `%%.64B`", fencedDirective)
	}
	return nil
}

func formatOperandOf(operandValue any) formatOperand {
	switch typedValue := operandValue.(type) {
	case string:
		return formatOperand{kind: textOperand, kindName: "text", textValue: typedValue}
	case bool:
		return formatOperand{kind: booleanOperand, kindName: "a boolean", booleanValue: typedValue}
	case nil:
		return formatOperand{kind: otherOperand, kindName: "null"}
	}
	reflectedValue := reflect.ValueOf(operandValue)
	switch reflectedValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: reflectedValue.Int()}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		unsignedValue := reflectedValue.Uint()
		if unsignedValue <= math.MaxInt64 {
			return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: int64(unsignedValue)}
		}
		return formatOperand{kind: numberOperand, kindName: numberOperandKinds, numberValue: float64(unsignedValue)}
	case reflect.Float32, reflect.Float64:
		return numberFormatOperand(reflectedValue.Float())
	case reflect.Slice, reflect.Array:
		return formatOperand{kind: otherOperand, kindName: "a list"}
	case reflect.Map:
		return formatOperand{kind: otherOperand, kindName: "a map"}
	}
	return formatOperand{kind: otherOperand, kindName: fmt.Sprintf("a Go %T", operandValue)}
}

func numberFormatOperand(numberValue float64) formatOperand {
	if math.IsNaN(numberValue) || math.IsInf(numberValue, 0) {
		return formatOperand{kind: otherOperand, kindName: "a number that is not finite"}
	}
	if numberValue == math.Trunc(numberValue) && numberValue >= math.MinInt64 && numberValue < math.MaxInt64 {
		return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: int64(numberValue)}
	}
	return formatOperand{kind: numberOperand, kindName: numberOperandKinds, numberValue: numberValue}
}

func operandIsAccepted(verbRule formatVerbRule, operand formatOperand) bool {
	switch operand.kind {
	case textOperand:
		return verbRule.acceptsText
	case wholeOperand:
		return verbRule.acceptsWhole
	case numberOperand:
		return verbRule.acceptsNumber
	case booleanOperand:
		return verbRule.acceptsBoolean
	}
	return false
}

func formattedVerbText(layoutVerb LayoutVerb, verbRule formatVerbRule, operand formatOperand, operandNumber int) (string, error) {
	if !operandIsAccepted(verbRule, operand) {
		return "", fmt.Errorf(
			"%s writes %s, and operand %d is %s",
			CodeSpan("%"+string(layoutVerb.Letter)), verbRule.acceptedKinds, operandNumber, operand.kindName,
		)
	}
	readText := operand.textValue
	if layoutVerb.Precision != nil && layoutVerb.Letter != 'B' {
		readText = leadingCodePoints(readText, *layoutVerb.Precision)
	}
	switch layoutVerb.Letter {
	case 's':
		return readText, nil
	case 'q':
		return goQuotedText(readText), nil
	case 'u':
		return strings.Map(unicode.ToUpper, readText), nil
	case 'l':
		return strings.Map(unicode.ToLower, readText), nil
	case 'A':
		return strings.ReplaceAll(readText, "/", "_"), nil
	case 'B':
		return tailBoundedText(readText, *layoutVerb.Precision), nil
	case 'v':
		return valueVerbText(operand), nil
	case 'd':
		return signedWholeText(strconv.FormatInt(operand.wholeValue, 10), layoutVerb.PlusSigned), nil
	case 'x', 'X':
		return hexadecimalText(operand, layoutVerb.Letter == 'X'), nil
	case 'f':
		return signedWholeText(fixedPointText(operand, layoutVerb), layoutVerb.PlusSigned), nil
	}
	return dateText(operand, operandNumber)
}

func leadingCodePoints(text string, count int) string {
	if utf8.RuneCountInString(text) <= count {
		return text
	}
	return string([]rune(text)[:count])
}

func tailBoundedText(text string, precision int) string {
	textRunes := []rune(text)
	if len(textRunes) <= precision {
		return text
	}
	return "-" + string(textRunes[len(textRunes)-(precision-1):])
}

func valueVerbText(operand formatOperand) string {
	switch operand.kind {
	case wholeOperand:
		return strconv.FormatInt(operand.wholeValue, 10)
	case booleanOperand:
		return strconv.FormatBool(operand.booleanValue)
	}
	return operand.textValue
}

func signedWholeText(numberText string, plusSigned bool) string {
	if plusSigned && !strings.HasPrefix(numberText, "-") {
		return "+" + numberText
	}
	return numberText
}

func hexadecimalText(operand formatOperand, upperCase bool) string {
	var hexadecimal string
	if operand.kind == textOperand {
		hexadecimal = fmt.Sprintf("%x", []byte(operand.textValue))
	} else {
		magnitude := uint64(operand.wholeValue)
		if operand.wholeValue < 0 {
			magnitude = uint64(-(operand.wholeValue + 1)) + 1
			hexadecimal = "-" + strconv.FormatUint(magnitude, 16)
		} else {
			hexadecimal = strconv.FormatUint(magnitude, 16)
		}
	}
	if upperCase {
		return strings.ToUpper(hexadecimal)
	}
	return hexadecimal
}

func fixedPointText(operand formatOperand, layoutVerb LayoutVerb) string {
	precision := 6
	if layoutVerb.Precision != nil {
		precision = *layoutVerb.Precision
	}
	if operand.kind == wholeOperand {
		wholeText := strconv.FormatInt(operand.wholeValue, 10)
		if precision == 0 {
			return wholeText
		}
		return wholeText + "." + strings.Repeat("0", precision)
	}
	return strconv.FormatFloat(operand.numberValue, 'f', precision, 64)
}

func dateText(operand formatOperand, operandNumber int) (string, error) {
	var seconds int64
	if operand.kind == wholeOperand {
		seconds = operand.wholeValue
	} else {
		flooredSeconds := math.Floor(operand.numberValue)
		if flooredSeconds < earliestFormatSeconds || flooredSeconds >= latestFormatSeconds {
			return "", dateRangeProblem(operandNumber)
		}
		seconds = int64(flooredSeconds)
	}
	if seconds < earliestFormatSeconds || seconds >= latestFormatSeconds {
		return "", dateRangeProblem(operandNumber)
	}
	days := seconds / secondsPerDay
	if seconds%secondsPerDay < 0 {
		days--
	}
	year, month, day := civilDateOfDays(days)
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day), nil
}

func dateRangeProblem(operandNumber int) error {
	return fmt.Errorf("`%%C` writes a time in the years 1 to 9999, and operand %d is outside them", operandNumber)
}

func civilDateOfDays(days int64) (int64, int64, int64) {
	shiftedDays := days + 719468
	era := shiftedDays / 146097
	dayOfEra := shiftedDays - era*146097
	yearOfEra := (dayOfEra - dayOfEra/1460 + dayOfEra/36524 - dayOfEra/146096) / 365
	dayOfYear := dayOfEra - (365*yearOfEra + yearOfEra/4 - yearOfEra/100)
	shiftedMonth := (5*dayOfYear + 2) / 153
	day := dayOfYear - (153*shiftedMonth+2)/5 + 1
	month := shiftedMonth + 3
	if shiftedMonth >= 10 {
		month = shiftedMonth - 9
	}
	year := yearOfEra + era*400
	if month <= 2 {
		year++
	}
	return year, month, day
}

func goQuotedText(text string) string {
	var quotedText strings.Builder
	quotedText.WriteByte('"')
	for _, textRune := range text {
		switch {
		case textRune == '"':
			quotedText.WriteString(`\"`)
		case textRune == '\\':
			quotedText.WriteString(`\\`)
		case textRune == '\a':
			quotedText.WriteString(`\a`)
		case textRune == '\b':
			quotedText.WriteString(`\b`)
		case textRune == '\f':
			quotedText.WriteString(`\f`)
		case textRune == '\n':
			quotedText.WriteString(`\n`)
		case textRune == '\r':
			quotedText.WriteString(`\r`)
		case textRune == '\t':
			quotedText.WriteString(`\t`)
		case textRune == '\v':
			quotedText.WriteString(`\v`)
		case textRune < 0x20 || textRune == 0x7f:
			fmt.Fprintf(&quotedText, `\x%02x`, textRune)
		case textRune >= 0x80 && textRune <= 0x9f:
			fmt.Fprintf(&quotedText, `\u%04x`, textRune)
		default:
			quotedText.WriteRune(textRune)
		}
	}
	quotedText.WriteByte('"')
	return quotedText.String()
}

func paddedVerbText(layoutVerb LayoutVerb, verbRule formatVerbRule, verbText string) string {
	if layoutVerb.Width == nil {
		return verbText
	}
	padding := *layoutVerb.Width - utf8.RuneCountInString(verbText)
	if padding <= 0 {
		return verbText
	}
	switch {
	case layoutVerb.LeftAligned:
		return verbText + strings.Repeat(" ", padding)
	case layoutVerb.ZeroPadded && verbRule.readsZeroPadding:
		signText := ""
		if strings.HasPrefix(verbText, "-") || strings.HasPrefix(verbText, "+") {
			signText, verbText = verbText[:1], verbText[1:]
		}
		return signText + strings.Repeat("0", padding) + verbText
	}
	return strings.Repeat(" ", padding) + verbText
}
