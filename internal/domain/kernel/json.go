package kernel

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JSON keeps structured domain payloads independent from any persistence library.
type JSON []byte

func NewJSON(value any) JSON {
	raw, _ := json.Marshal(value)
	return JSON(raw)
}

func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	return string(j), nil
}

func (j *JSON) Scan(value any) error {
	if j == nil {
		return errors.New("domain.JSON: scan on nil pointer")
	}
	switch typed := value.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[0:0], typed...)
	case string:
		*j = append((*j)[0:0], typed...)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		*j = append((*j)[0:0], raw...)
	}
	return nil
}

func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(j) {
		return nil, errors.New("domain.JSON: invalid JSON")
	}
	return j, nil
}

func (j *JSON) UnmarshalJSON(raw []byte) error {
	if j == nil {
		return errors.New("domain.JSON: unmarshal on nil pointer")
	}
	if len(raw) == 0 {
		*j = nil
		return nil
	}
	if !json.Valid(raw) {
		return errors.New("domain.JSON: invalid JSON")
	}
	*j = append((*j)[0:0], raw...)
	return nil
}
