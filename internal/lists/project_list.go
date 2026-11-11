package lists

// ProjectListName is the file name of the project list at the root of a checkout.
const ProjectListName = ".heavy.json"

// ProjectListForm is the shape a .heavy.json must have, as error messages name it.
const ProjectListForm = `{"commands": [...]} with each command a non-empty string like "make ci", or an object like ` +
	`{"command": "checkall.sh", "unless": ["CHECKALL_CASES="]} with at least one of "unless" (single words, ` +
	`a word ending in "=" as NAME=), "red" (non-empty RE2 patterns like "^First red in "), "exclusive" and "restart" (true or false)`

// LoadProjectList returns the commands of a .heavy.json, each split into words with its exceptions, patterns and flags.
func LoadProjectList(path string) ([]Entry, error) {
	document, err := readJSON(path, ProjectListForm)
	if err != nil {
		return nil, err
	}
	fields, isObject := decodeObject(document.data)
	if !isObject || len(fields) != 1 {
		return nil, document.shapeError()
	}
	commands, found := fields["commands"]
	if !found {
		return nil, document.shapeError()
	}
	return decodeEntries(document, commands)
}

// decodeEntries reads a list of commands of a document.
func decodeEntries(document document, raw []byte) ([]Entry, error) {
	values, isArray := decodeArray(raw)
	if !isArray {
		return nil, document.shapeError()
	}
	decoded := make([]rawEntry, 0, len(values))
	for _, value := range values {
		entry, valid := decodeEntry(value)
		if !valid {
			return nil, document.shapeError()
		}
		decoded = append(decoded, entry)
	}
	var entries []Entry
	for _, raw := range decoded {
		entry, err := raw.compile(document.path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
