package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Load reads one strict baseline JSON document. It rejects unknown or
// duplicate object fields, empty identity values, duplicate identities, and
// trailing JSON. Paths are identities only; Load does not inspect the file
// system because stale entries commonly refer to files that no longer exist.
func Load(reader io.Reader) (Baseline, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Baseline{}, fmt.Errorf("read baseline: %w", err)
	}
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}
	encodedEntries, exists := fields["entries"]
	if !exists {
		return Baseline{}, errors.New("decode baseline: entries is required")
	}
	if len(bytes.TrimSpace(encodedEntries)) == 0 || bytes.TrimSpace(encodedEntries)[0] != '[' {
		return Baseline{}, errors.New("decode baseline: entries must be an array")
	}
	if err := rejectNullTargets(encodedEntries); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}

	var baseline Baseline
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}
	if err := requireEndOfJSON(decoder); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}
	if baseline.Entries == nil {
		baseline.Entries = []Entry{}
	}
	if err := validateEntries(baseline.Entries); err != nil {
		return Baseline{}, err
	}

	return baseline, nil
}

// Write emits a validated baseline as stable, indented JSON. It does not
// mutate the supplied Baseline.
func Write(writer io.Writer, baseline Baseline) error {
	if err := validateEntries(baseline.Entries); err != nil {
		return err
	}

	canonical := Baseline{Entries: slices.Clone(baseline.Entries)}
	if canonical.Entries == nil {
		canonical.Entries = []Entry{}
	}
	sortEntries(canonical.Entries)

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(canonical); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}

	return nil
}

func validateEntries(entries []Entry) error {
	seen := make(map[identity]int, len(entries))
	for index, entry := range entries {
		if entry.Rule == "" {
			return fmt.Errorf("baseline entry %d: rule must not be empty", index)
		}
		if entry.From == "" {
			return fmt.Errorf("baseline entry %d: from must not be empty", index)
		}
		if entry.To != nil && *entry.To == "" {
			return fmt.Errorf("baseline entry %d: to must not be empty", index)
		}

		key := identityFromEntry(entry)
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("baseline entry %d duplicates entry %d", index, previous)
		}
		seen[key] = index
	}

	return nil
}

func rejectNullTargets(encodedEntries []byte) error {
	var fields []map[string]json.RawMessage
	if err := json.Unmarshal(encodedEntries, &fields); err != nil {
		return err
	}
	for index, entry := range fields {
		to, exists := entry["to"]
		if exists && bytes.Equal(bytes.TrimSpace(to), []byte("null")) {
			return fmt.Errorf("baseline entry %d: to must be a string when present", index)
		}
	}

	return nil
}

func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := readJSONValue(decoder); err != nil {
		return err
	}

	return requireEndOfJSON(decoder)
}

func readJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, tokenErr := decoder.Token()
			if tokenErr != nil {
				return tokenErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
	case '[':
		for decoder.More() {
			if err := readJSONValue(decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}

	return nil
}

func requireEndOfJSON(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value is not allowed")
		}

		return err
	}

	return nil
}
