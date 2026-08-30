//go:build ignore

package pricing

func UpdateRateEntry(req *UpdateReq, entry *RateEntry) {
	entry.LaborRateText = req.GetLaborRateText() // want: money-text-fields-must-read-back-from-db
}
