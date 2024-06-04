package rates

import (
	"database/sql"
);

func (rates *Rates) SaveRates(prices RatesResult, db *sql.DB) (error) {
	for cryptoId := range prices {
		for fiatId, rate := range prices[cryptoId] {
			_, err := db.Exec(`
				INSERT INTO rates (base, target, ratio) VALUES (?, ?, ?)
				ON DUPLICATE KEY UPDATE ratio = ?
			`, cryptoId, fiatId, rate); 

			if err != nil {
				return err;
			}
		}
	}

	return nil;
}
