package chaintrail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
)

// canonicalJSON produces deterministic JSON for hashing. Objects are sorted,
// insignificant whitespace is removed, and numerically equivalent JSON numbers
// normalize to the same scientific representation without losing precision.
func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("invalid JSON: multiple top-level values")
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		encoded, _ := json.Marshal(v)
		out.Write(encoded)
	case json.Number:
		normalized, err := normalizeNumber(v.String())
		if err != nil {
			return err
		}
		out.WriteString(normalized)
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			out.Write(encoded)
			out.WriteByte(':')
			if err := writeCanonical(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func normalizeNumber(input string) (string, error) {
	negative := strings.HasPrefix(input, "-")
	if negative {
		input = input[1:]
	}

	mantissa := input
	exponentText := "0"
	if idx := strings.IndexAny(input, "eE"); idx >= 0 {
		mantissa = input[:idx]
		exponentText = input[idx+1:]
	}
	if strings.HasPrefix(exponentText, "+") {
		exponentText = exponentText[1:]
	}
	exponent := new(big.Int)
	if _, ok := exponent.SetString(exponentText, 10); !ok {
		return "", fmt.Errorf("invalid JSON number %q", input)
	}

	fractionDigits := 0
	digits := mantissa
	if idx := strings.IndexByte(mantissa, '.'); idx >= 0 {
		fractionDigits = len(mantissa) - idx - 1
		digits = mantissa[:idx] + mantissa[idx+1:]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0", nil
	}

	trailing := len(digits) - len(strings.TrimRight(digits, "0"))
	if trailing > 0 {
		digits = digits[:len(digits)-trailing]
	}

	scale := new(big.Int).Set(exponent)
	scale.Sub(scale, big.NewInt(int64(fractionDigits)))
	scale.Add(scale, big.NewInt(int64(trailing)))
	scientificExponent := new(big.Int).Set(scale)
	scientificExponent.Add(scientificExponent, big.NewInt(int64(len(digits)-1)))

	var out strings.Builder
	if negative {
		out.WriteByte('-')
	}
	out.WriteByte(digits[0])
	if len(digits) > 1 {
		out.WriteByte('.')
		out.WriteString(digits[1:])
	}
	out.WriteByte('e')
	out.WriteString(scientificExponent.String())
	return out.String(), nil
}
