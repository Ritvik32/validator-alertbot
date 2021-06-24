package targets

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"validator-alertbot/config"

	client "github.com/influxdata/influxdb1-client/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// GetAccountInfo to get account balance information using account address
func GetAccountInfo(ops HTTPOptions, cfg *config.Config, c client.Client) {
	bp, err := createBatchPoints(cfg.InfluxDB.Database)
	if err != nil {
		return
	}

	resp, err := HitHTTPTarget(ops)
	if err != nil {
		log.Printf("Error in get account info: %v", err)
		return
	}

	var accResp AccountBalance
	err = json.Unmarshal(resp.Body, &accResp)
	if err != nil {
		log.Printf("Error while unmarshelling AccountResp: %v", err)
		return
	}

	var amount, denom string

	for _, value := range accResp.Balances {
		if value.Denom == cfg.BalanceDenom {
			amount = value.Amount
			denom = value.Denom
			break
		}
	}

	if len(accResp.Balances) > 0 {
		// amount := accResp.Balances[0].Amount
		// denom := accResp.Balances[0].Denom
		prevAmount := GetAccountBalFromDb(cfg, c)

		if prevAmount != amount {
			if strings.ToUpper(cfg.BalanceChangeAlerts) == "YES" {
				amount1 := ConvertToFolat64(prevAmount)
				amount2 := ConvertToFolat64(amount)
				balChange := amount1 - amount2
				if balChange < 0 {
					balChange = -(balChange)
				}
				if balChange > cfg.DelegationAlerts.AccBalanceChangeThreshold {
					a1 := convertToCommaSeparated(fmt.Sprintf("%f", amount1)) + cfg.Denom
					a2 := convertToCommaSeparated(fmt.Sprintf("%f", amount2)) + cfg.Denom
					_ = SendTelegramAlert(fmt.Sprintf("Your account balance has changed from  %s to %s", a1, a2), cfg)
					_ = SendEmailAlert(fmt.Sprintf("Your account balance has changed from  %s to %s", a1, a2), cfg)
				}
			}
		}

		_ = writeToInfluxDb(c, bp, "vab_account_balance", map[string]string{}, map[string]interface{}{"balance": amount, "denom": denom})
		log.Printf("Address Balance: %s \t and denom : %s", amount, denom)
	}
}

func GetSelfDelegation(cfg *config.Config) (string, error) {
	var ops HTTPOptions
	ops = HTTPOptions{
		Endpoint: cfg.LCDEndpoint + "/cosmos/staking/v1beta1/delegations/" + cfg.AccountAddress,
		Method:   http.MethodGet,
	}
	var selfDelegated string

	resp, err := GetSelfDelegationResp(ops)
	if err != nil {
		log.Printf("Error while getting self delegations response : %v", err)
		return selfDelegated, err
	}

	for _, v := range resp.DelegationResponses {
		if v.Delegation.ValidatorAddress == cfg.ValOperatorAddress {
			s := v.Balance.Amount
			a := ConvertToFolat64(s)
			selfDelegated = convertToCommaSeparated(fmt.Sprintf("%f", a)) + cfg.Denom
		}
	}

	log.Printf("selfdelegated amout : %s", selfDelegated)

	return selfDelegated, nil
}

func GetSelfDelegationResp(ops HTTPOptions) (SelfDelegations, error) {
	var delegation SelfDelegations
	resp, err := HitHTTPTarget(ops)
	if err != nil {
		log.Printf("Error in get account info: %v", err)
		return delegation, err
	}

	err = json.Unmarshal(resp.Body, &delegation)
	if err != nil {
		log.Printf("Error while unmarshelling self delegation response: %v", err)
		return delegation, err
	}

	return delegation, nil
}

func GetUndelegatedRes(ops HTTPOptions) (Undelegation, error) {
	var undelegated Undelegation
	resp, err := HitHTTPTarget(ops)
	if err != nil {
		log.Printf("Error in get account info: %v", err)
		return undelegated, err
	}

	err = json.Unmarshal(resp.Body, &undelegated)
	if err != nil {
		log.Printf("Error while unmarshelling AccountResp: %v", err)
		return undelegated, err
	}

	return undelegated, nil
}

func GetUndelegated(cfg *config.Config) (string, error) {
	var ops HTTPOptions
	ops = HTTPOptions{
		Endpoint: cfg.LCDEndpoint + "/cosmos/staking/v1beta1/delegators/" + cfg.AccountAddress + "/unbonding_delegations",
		Method:   http.MethodGet,
	}
	var unDelegated string

	resp, err := GetUndelegatedRes(ops)
	if err != nil {
		log.Printf("Error while getting self delegations response : %v", err)
		return unDelegated, err
	}

	for _, v := range resp.UnbondingResponses {
		if v.Undelegation.ValidatorAddress == cfg.ValOperatorAddress {
			s := v.Balance.Amount
			a := ConvertToFolat64(s)
			unDelegated = convertToCommaSeparated(fmt.Sprintf("%f", a)) + cfg.Denom
		}
	}

	log.Printf("Unbonding delegations : %s", unDelegated)

	return unDelegated, nil
}

// ConvertToAKT converts balance from uakt to AKT
func ConvertToAKT(balance string, denom string) string {
	bal, _ := strconv.ParseFloat(balance, 64)

	a1 := bal / math.Pow(10, 6)
	amount := fmt.Sprintf("%.6f", a1)

	return convertToCommaSeparated(amount) + denom
}

// GetAccountBalFromDb returns account balance from db
func GetAccountBalFromDb(cfg *config.Config, c client.Client) string {
	var balance string
	q := client.NewQuery("SELECT last(balance) FROM vab_account_balance", cfg.InfluxDB.Database, "")
	if response, err := c.Query(q); err == nil && response.Error() == nil {
		for _, r := range response.Results {
			if len(r.Series) != 0 {
				for idx, col := range r.Series[0].Columns {
					if col == "last" {
						amount := r.Series[0].Values[0][idx]
						balance = fmt.Sprintf("%v", amount)
						break
					}
				}
			}
		}
	}
	return balance
}

func convertToCommaSeparated(amt string) string {
	a, err := strconv.Atoi(amt)
	if err != nil {
		return amt
	}
	p := message.NewPrinter(language.English)
	return p.Sprintf("%d", a)
}

// ConvertToFolat64 converts balance from string to float64
func ConvertToFolat64(balance string) float64 {
	bal, _ := strconv.ParseFloat(balance, 64)

	a1 := bal / math.Pow(10, 6)
	amount := fmt.Sprintf("%.6f", a1)

	a, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		log.Printf("Error while converting string to folat64 : %v", err)
	}

	return a
}
