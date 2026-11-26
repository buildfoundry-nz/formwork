//go:build ignore

package pricing

func CreatePriceEntry(req *SubmitReq) *RateEntry {
	return &RateEntry{
		TariffText: req.GetRateInput(), // want: money-text-fields-must-read-back-from-db
	}
}
