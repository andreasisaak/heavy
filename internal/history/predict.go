package history

import "sort"

// predictionRecords is how many of the newest matching records a prediction looks at.
const predictionRecords = 5

// Predict is the highest memory peak of the newest five records of the command in the checkout that measured one;
// without such a record of the command, of the newest five of its label, since one short name covers commands as
// different as `make test-complete TEST_WORKERS=1` and a measurement on seven test servers. known is false without
// any record.
func Predict(directory, command, label, checkout string) (peak int64, known bool, err error) {
	records, err := Read(directory)
	if err != nil {
		return 0, false, err
	}
	peak, known = highest(records, func(current Record) bool { return current.Command == command && current.Checkout == checkout })
	if known {
		return peak, true, nil
	}
	peak, known = highest(records, func(current Record) bool { return current.Label == label && current.Checkout == checkout })
	return peak, known, nil
}

// highest is the highest peak of the newest five records that match and measured one.
func highest(records []Record, matches func(Record) bool) (peak int64, known bool) {
	var matching []Record
	for _, current := range records {
		if matches(current) && current.PeakRSSBytes > 0 {
			matching = append(matching, current)
		}
	}
	sort.SliceStable(matching, func(left, right int) bool { return matching[left].Ended < matching[right].Ended })
	if len(matching) > predictionRecords {
		matching = matching[len(matching)-predictionRecords:]
	}
	for _, current := range matching {
		peak = max(peak, current.PeakRSSBytes)
	}
	return peak, len(matching) > 0
}
