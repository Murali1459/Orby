package aerospike

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	as "github.com/aerospike/aerospike-client-go/v8"
	"orby/plugins"
)

type expressionNode struct {
	Kind     string           `json:"kind"`
	Logic    string           `json:"logic,omitempty"`
	Children []expressionNode `json:"children,omitempty"`
	Bin      string           `json:"bin,omitempty"`
	Type     string           `json:"type,omitempty"`
	Operator string           `json:"operator,omitempty"`
	Value    string           `json:"value,omitempty"`
}

type command struct {
	Namespace  string
	Set        string
	PrimaryKey string
	Limit      int
	Metadata   bool
	Filter     *as.Expression
}

func parseCommand(request plugins.Request) (command, error) {
	value := command{
		Namespace:  strings.TrimSpace(request.Fields["namespace"]),
		Set:        strings.TrimSpace(request.Fields["set"]),
		PrimaryKey: strings.TrimSpace(request.Fields["primaryKey"]),
		Limit:      100,
	}
	if value.Namespace == "" {
		return command{}, fmt.Errorf("namespace is required")
	}
	if value.Set == "" {
		return command{}, fmt.Errorf("set is required")
	}
	if rawLimit := strings.TrimSpace(request.Fields["limit"]); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit == 0 || limit < -1 {
			return command{}, fmt.Errorf("limit must be a positive integer or -1")
		}
		value.Limit = limit
	}
	if rawMetadata := strings.TrimSpace(request.Fields["metadata"]); rawMetadata != "" {
		metadata, err := strconv.ParseBool(rawMetadata)
		if err != nil {
			return command{}, fmt.Errorf("metadata must be true or false")
		}
		value.Metadata = metadata
	}
	raw := strings.TrimSpace(request.Fields["expression"])
	if raw == "" {
		return value, nil
	}
	var root expressionNode
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&root); err != nil {
		return command{}, fmt.Errorf("invalid expression: %v", err)
	}
	filter, err := compileExpression(root)
	if err != nil {
		return command{}, err
	}
	value.Filter = filter
	return value, nil
}

func compileExpression(node expressionNode) (*as.Expression, error) {
	switch node.Kind {
	case "group":
		logic := strings.ToLower(strings.TrimSpace(node.Logic))
		if logic != "and" && logic != "or" {
			return nil, fmt.Errorf("expression group requires AND or OR")
		}
		if len(node.Children) == 0 {
			return nil, fmt.Errorf("expression group cannot be empty")
		}
		if len(node.Children) == 1 {
			return compileExpression(node.Children[0])
		}
		children := make([]*as.Expression, len(node.Children))
		for index, child := range node.Children {
			compiled, err := compileExpression(child)
			if err != nil {
				return nil, err
			}
			children[index] = compiled
		}
		if logic == "and" {
			return as.ExpAnd(children...), nil
		}
		return as.ExpOr(children...), nil
	case "condition":
		return compileCondition(node)
	default:
		return nil, fmt.Errorf("expression node kind must be group or condition")
	}
}

func compileCondition(node expressionNode) (*as.Expression, error) {
	bin, dataType, operator := strings.TrimSpace(node.Bin), strings.ToLower(strings.TrimSpace(node.Type)), strings.ToLower(strings.TrimSpace(node.Operator))
	if bin == "" {
		return nil, fmt.Errorf("expression bin is required")
	}
	var left, right *as.Expression
	switch dataType {
	case "string":
		left, right = as.ExpStringBin(bin), as.ExpStringVal(node.Value)
	case "integer":
		value, err := strconv.ParseInt(strings.TrimSpace(node.Value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s requires an integer value", bin)
		}
		left, right = as.ExpIntBin(bin), as.ExpIntVal(value)
	case "float":
		value, err := strconv.ParseFloat(strings.TrimSpace(node.Value), 64)
		if err != nil {
			return nil, fmt.Errorf("%s requires a float value", bin)
		}
		left, right = as.ExpFloatBin(bin), as.ExpFloatVal(value)
	case "boolean":
		value, err := strconv.ParseBool(strings.TrimSpace(node.Value))
		if err != nil {
			return nil, fmt.Errorf("%s requires true or false", bin)
		}
		if operator != "eq" && operator != "ne" {
			return nil, fmt.Errorf("boolean expressions support only equality")
		}
		left, right = as.ExpBoolBin(bin), as.ExpBoolVal(value)
	default:
		return nil, fmt.Errorf("expression bin type is required")
	}
	comparisons := map[string]func(*as.Expression, *as.Expression) *as.Expression{
		"eq": as.ExpEq, "ne": as.ExpNotEq, "gt": as.ExpGreater,
		"gte": as.ExpGreaterEq, "lt": as.ExpLess, "lte": as.ExpLessEq,
	}
	comparison := comparisons[operator]
	if comparison == nil {
		return nil, fmt.Errorf("unsupported expression operator %q", node.Operator)
	}
	return comparison(left, right), nil
}
