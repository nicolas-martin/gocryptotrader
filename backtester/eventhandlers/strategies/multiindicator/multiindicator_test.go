package multiindicator

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thrasher-corp/gocryptotrader/currency"
	"github.com/thrasher-corp/gocryptotrader/exchanges/asset"
	"github.com/thrasher-corp/gocryptotrader/exchanges/futures"
	"github.com/thrasher-corp/gocryptotrader/exchanges/order"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventtypes/event"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventtypes/signal"
)

func TestName(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	assert.Equal(t, Name, s.Name())
	assert.Equal(t, "multiindicator", s.Name())
}

func TestDescription(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	assert.Contains(t, s.Description(), "Multi-indicator strategy")
	assert.Contains(t, s.Description(), "EMA trend filter")
}

func TestSupportsSimultaneousProcessing(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	assert.True(t, s.SupportsSimultaneousProcessing())
}

func TestSetDefaults(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	assert.Equal(t, decimal.NewFromInt(50), s.emaFastPeriod)
	assert.Equal(t, decimal.NewFromInt(200), s.emaSlowPeriod)
	assert.Equal(t, decimal.NewFromInt(14), s.rsiPeriod)
	assert.Equal(t, decimal.NewFromInt(40), s.rsiLongTrig)
	assert.Equal(t, decimal.NewFromInt(70), s.rsiExitOB)
	assert.Equal(t, decimal.NewFromInt(20), s.bbPeriod)
	assert.Equal(t, decimal.NewFromFloat(2.0), s.bbStdDev)
	assert.Equal(t, decimal.NewFromInt(10), s.obvSmooth)
	assert.Equal(t, decimal.NewFromInt(10), s.lookbackPeriods)
}

func TestSetCustomSettings(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	customSettings := map[string]interface{}{
		"ema-fast-period":     30.0,
		"ema-slow-period":     100.0,
		"rsi-period":          21.0,
		"rsi-long-trigger":    35.0,
		"rsi-exit-overbought": 75.0,
		"bb-period":           25.0,
		"bb-std-dev":          2.5,
		"obv-smooth-period":   15.0,
		"lookback-periods":    12.0,
	}
	
	err := s.SetCustomSettings(customSettings)
	require.NoError(t, err)
	
	assert.True(t, s.emaFastPeriod.Equal(decimal.NewFromFloat(30)))
	assert.True(t, s.emaSlowPeriod.Equal(decimal.NewFromFloat(100)))
	assert.True(t, s.rsiPeriod.Equal(decimal.NewFromFloat(21)))
	assert.True(t, s.rsiLongTrig.Equal(decimal.NewFromFloat(35)))
	assert.True(t, s.rsiExitOB.Equal(decimal.NewFromFloat(75)))
	assert.True(t, s.bbPeriod.Equal(decimal.NewFromFloat(25)))
	assert.True(t, s.bbStdDev.Equal(decimal.NewFromFloat(2.5)))
	assert.True(t, s.obvSmooth.Equal(decimal.NewFromFloat(15)))
	assert.True(t, s.lookbackPeriods.Equal(decimal.NewFromFloat(12)))
}

func TestSetCustomSettingsInvalidType(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	
	customSettings := map[string]interface{}{
		"ema-fast-period": "invalid", // Should be float64
	}
	
	err := s.SetCustomSettings(customSettings)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid ema-fast-period value")
}

func TestSetCustomSettingsUnknownSetting(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	
	customSettings := map[string]interface{}{
		"unknown-setting": 42.0,
	}
	
	err := s.SetCustomSettings(customSettings)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown custom setting")
}

func TestCheckBBLowerTouch(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	// Test case where price touched lower band
	closes := []float64{100, 99, 98, 95, 97, 100, 102} // Price at 95 touches lower band
	lower := []float64{96, 96, 96, 96, 96, 96, 96}    // Lower band at 96
	
	touched := s.checkBBLowerTouch(closes, lower)
	assert.True(t, touched, "Should detect lower band touch")
	
	// Test case where price never touched lower band
	closes2 := []float64{100, 99, 98, 97, 98, 100, 102}
	lower2 := []float64{96, 96, 96, 96, 96, 96, 96}
	
	touched2 := s.checkBBLowerTouch(closes2, lower2)
	assert.False(t, touched2, "Should not detect lower band touch")
}

func TestHasOpenPosition(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	
	cp := currency.NewPair(currency.BTC, currency.USD)
	
	// Create mock signal with Base field initialized
	mockSignal := &signal.Signal{
		Base: event.Base{
			Exchange:     "kraken",
			AssetType:    asset.Spot,
			CurrencyPair: cp,
		},
	}
	
	// Test with position
	positions := []futures.Position{
		{
			Exchange:    "kraken",
			Asset:       asset.Spot,
			Pair:        cp,
			LatestSize:  decimal.NewFromFloat(0.1), // Meaningful position
		},
	}
	
	hasPos := s.hasOpenPosition(positions, mockSignal)
	assert.True(t, hasPos, "Should detect open position")
	
	// Test with dust position
	positions[0].LatestSize = decimal.NewFromFloat(0.000001) // Dust amount
	hasPos = s.hasOpenPosition(positions, mockSignal)
	assert.False(t, hasPos, "Should ignore dust positions")
	
	// Test with no positions
	emptyPositions := []futures.Position{}
	hasPos = s.hasOpenPosition(emptyPositions, mockSignal)
	assert.False(t, hasPos, "Should detect no positions")
}

func TestMassageMissingData(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	// Test normal data
	normalData := []decimal.Decimal{
		decimal.NewFromFloat(100),
		decimal.NewFromFloat(101),
		decimal.NewFromFloat(102),
	}
	
	result, err := s.massageMissingData(normalData, nowTime())
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, 100.0, result[0])
	assert.Equal(t, 101.0, result[1])
	assert.Equal(t, 102.0, result[2])
}

func TestEvaluateExitConditions(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	// Create a test signal
	es := &signal.Signal{}
	
	// Test RSI overbought reversal
	latestRSI := decimal.NewFromFloat(68)
	prevRSI := decimal.NewFromFloat(72)
	latestClose := decimal.NewFromFloat(50000)
	latestBBMiddle := decimal.NewFromFloat(49000)
	
	s.evaluateExitConditions(es, latestRSI, prevRSI, latestClose, latestBBMiddle)
	assert.Equal(t, order.Sell, es.GetDirection())
	
	// Reset and test structure breakdown
	es = &signal.Signal{}
	latestRSI = decimal.NewFromFloat(60)
	prevRSI = decimal.NewFromFloat(62)
	latestClose = decimal.NewFromFloat(48000) // Below BB middle
	latestBBMiddle = decimal.NewFromFloat(49000)
	
	s.evaluateExitConditions(es, latestRSI, prevRSI, latestClose, latestBBMiddle)
	assert.Equal(t, order.Sell, es.GetDirection())
	
	// Test no exit conditions
	es = &signal.Signal{}
	latestRSI = decimal.NewFromFloat(60)
	prevRSI = decimal.NewFromFloat(58)
	latestClose = decimal.NewFromFloat(50000) // Above BB middle
	latestBBMiddle = decimal.NewFromFloat(49000)
	
	s.evaluateExitConditions(es, latestRSI, prevRSI, latestClose, latestBBMiddle)
	assert.Equal(t, order.DoNothing, es.GetDirection())
}

func TestEvaluateEntryConditions(t *testing.T) {
	t.Parallel()
	s := Strategy{}
	s.SetDefaults()
	
	// Test all conditions met for entry
	es := &signal.Signal{}
	emaFast := decimal.NewFromFloat(50100)
	emaSlow := decimal.NewFromFloat(50000)    // Trend up
	latestRSI := decimal.NewFromFloat(42)     // Above trigger
	prevRSI := decimal.NewFromFloat(38)       // Cross up
	latestClose := decimal.NewFromFloat(50200)
	latestBBMiddle := decimal.NewFromFloat(50000) // Above middle
	obvSlope := decimal.NewFromFloat(1000)        // Positive
	touchedLowerRecently := true                  // Structure OK
	
	s.evaluateEntryConditions(es, emaFast, emaSlow, latestRSI, prevRSI, 
		latestClose, latestBBMiddle, obvSlope, touchedLowerRecently)
	
	assert.Equal(t, order.Buy, es.GetDirection())
	assert.True(t, es.GetBuyLimit().GreaterThan(latestClose))
	
	// Test conditions not met
	es = &signal.Signal{}
	emaFast = decimal.NewFromFloat(49900) // Trend down
	emaSlow = decimal.NewFromFloat(50000)
	
	s.evaluateEntryConditions(es, emaFast, emaSlow, latestRSI, prevRSI, 
		latestClose, latestBBMiddle, obvSlope, touchedLowerRecently)
	
	assert.Equal(t, order.DoNothing, es.GetDirection())
}

// Helper functions for testing
func nowTime() time.Time {
	return time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
}