package rule

import (
	"fmt"
	"reflect"
	"strconv"
)

// Operator represents a comparison operator.
type Operator string

const (
	OpEqual        Operator = "=="
	OpNotEqual     Operator = "!="
	OpGreater      Operator = ">"
	OpLess         Operator = "<"
	OpGreaterEqual Operator = ">="
	OpLessEqual    Operator = "<="
)

// Condition represents a single evaluated condition.
type Condition struct {
	Operator Operator
	Value    interface{}
	Not      bool // Invert result
}

// Evaluate checks if the given value satisfies the condition.
func (c *Condition) Evaluate(value interface{}) (bool, error) {
	targetFloat, targetIsNum := toFloat64(c.Value)
	valueFloat, valueIsNum := toFloat64(value)

	var result bool
	var err error

	if targetIsNum && valueIsNum {
		result = c.compareFloat(valueFloat, targetFloat)
	} else {
		switch c.Operator {
		case OpEqual:
			result = reflect.DeepEqual(value, c.Value)
		case OpNotEqual:
			result = !reflect.DeepEqual(value, c.Value)
		default:
			return false, fmt.Errorf("operator %s not supported for non-numeric types", c.Operator)
		}
	}

	if err != nil {
		return false, err
	}

	if c.Not {
		return !result, nil
	}
	return result, nil
}

func (c *Condition) compareFloat(value, target float64) bool {
	switch c.Operator {
	case OpEqual:
		return value == target
	case OpNotEqual:
		return value != target
	case OpGreater:
		return value > target
	case OpLess:
		return value < target
	case OpGreaterEqual:
		return value >= target
	case OpLessEqual:
		return value <= target
	default:
		return false
	}
}

// toFloat64 converts a value to float64 if possible.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	case bool:
		if val {
			return 1, true
		}
		return 0, true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// ParseOperator converts a string to an Operator.
func ParseOperator(s string) (Operator, error) {
	switch s {
	case "==":
		return OpEqual, nil
	case "!=":
		return OpNotEqual, nil
	case ">":
		return OpGreater, nil
	case "<":
		return OpLess, nil
	case ">=":
		return OpGreaterEqual, nil
	case "<=":
		return OpLessEqual, nil
	default:
		return "", fmt.Errorf("unknown operator: %s", s)
	}
}

// ValidOperators returns a list of valid operator strings.
func ValidOperators() []string {
	return []string{"==", "!=", ">", "<", ">=", "<="}
}
