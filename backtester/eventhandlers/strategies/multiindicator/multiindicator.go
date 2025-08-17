package multiindicator

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/thrasher-corp/gct-ta/indicators"

	"github.com/thrasher-corp/gocryptotrader/backtester/common"
	"github.com/thrasher-corp/gocryptotrader/backtester/data"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/portfolio"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/strategies/base"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventtypes/signal"
	"github.com/thrasher-corp/gocryptotrader/backtester/funding"
	"github.com/thrasher-corp/gocryptotrader/exchanges/order"
)

const (
	// Name is the strategy name
	Name = "multiindicator"
	
	// Configuration keys for custom settings
	emaFastPeriodKey   = "ema-fast-period"     // Default: 50
	emaSlowPeriodKey   = "ema-slow-period"     // Default: 200
	rsiPeriodKey       = "rsi-period"          // Default: 14
	rsiLongTrigKey     = "rsi-long-trigger"    // Default: 40
	rsiExitOBKey       = "rsi-exit-overbought" // Default: 70
	bbPeriodKey        = "bb-period"           // Default: 20
	bbStdDevKey        = "bb-std-dev"          // Default: 2.0
	obvSmoothKey       = "obv-smooth-period"   // Default: 10
	lookbackPeriodsKey = "lookback-periods"    // Default: 10 (for BB touch detection)
	
	description = `Multi-indicator strategy combining EMA trend filter (50/200), RSI momentum (14), 
Bollinger Bands structure, and OBV volume confirmation for Kraken spot trading. Uses GCT's 
built-in risk management for position sizing and stop losses.`
)

// Strategy implements the Handler interface for multi-indicator trading
type Strategy struct {
	base.Strategy
	emaFastPeriod   decimal.Decimal
	emaSlowPeriod   decimal.Decimal
	rsiPeriod       decimal.Decimal
	rsiLongTrig     decimal.Decimal
	rsiExitOB       decimal.Decimal
	bbPeriod        decimal.Decimal
	bbStdDev        decimal.Decimal
	obvSmooth       decimal.Decimal
	lookbackPeriods decimal.Decimal
	
	// State tracking for condition changes
	prevTrendUp     bool
	prevRSIMomentum bool
	prevStructureOK bool
	prevVolumeOK    bool
	prevConditionCount int
}

// Name returns the name of the strategy
func (s *Strategy) Name() string {
	return Name
}

// Description provides an overview of the strategy
func (s *Strategy) Description() string {
	return description
}

// OnSignal handles a data event and returns what action the strategy believes should occur
// This implements the core multi-indicator logic following GCT signal patterns
func (s *Strategy) OnSignal(d data.Handler, _ funding.IFundingTransferer, p portfolio.Handler) (signal.Event, error) {
	if d == nil {
		return nil, common.ErrNilEvent
	}
	
	es, err := s.GetBaseData(d)
	if err != nil {
		return nil, err
	}

	latest, err := d.Latest()
	if err != nil {
		return nil, err
	}

	es.SetPrice(latest.GetClosePrice())

	// Check if we have enough data for all indicators (use slowest EMA)
	minPeriod := s.emaSlowPeriod.IntPart()
	if offset := latest.GetOffset(); offset <= minPeriod {
		es.AppendReasonf("Not enough data for signal generation, need %d periods, have %d", 
			minPeriod, offset)
		es.SetDirection(order.DoNothing)
		return &es, nil
	}

	// Get OHLCV data streams
	closes, err := d.StreamClose()
	if err != nil {
		return nil, err
	}
	
	volumes, err := d.StreamVol()
	if err != nil {
		return nil, err
	}

	// Massage data to handle missing values
	processedCloses, err := s.massageMissingData(closes, es.GetTime())
	if err != nil {
		return nil, err
	}
	
	processedVolumes, err := s.massageMissingData(volumes, es.GetTime())
	if err != nil {
		return nil, err
	}

	// Calculate all indicators using gct-ta library
	emaFast := indicators.EMA(processedCloses, int(s.emaFastPeriod.IntPart()))
	emaSlow := indicators.EMA(processedCloses, int(s.emaSlowPeriod.IntPart()))
	rsi := indicators.RSI(processedCloses, int(s.rsiPeriod.IntPart()))
	_, middle, lower := indicators.BBANDS(processedCloses, int(s.bbPeriod.IntPart()), 
		s.bbStdDev.InexactFloat64(), s.bbStdDev.InexactFloat64(), indicators.Sma)
	obv := indicators.OBV(processedCloses, processedVolumes)
	obvSmoothed := indicators.EMA(obv, int(s.obvSmooth.IntPart()))

	// Get latest indicator values
	latestClose := decimal.NewFromFloat(processedCloses[len(processedCloses)-1])
	latestEMAFast := decimal.NewFromFloat(emaFast[len(emaFast)-1])
	latestEMASlow := decimal.NewFromFloat(emaSlow[len(emaSlow)-1])
	latestRSI := decimal.NewFromFloat(rsi[len(rsi)-1])
	latestBBMiddle := decimal.NewFromFloat(middle[len(middle)-1])

	// Get previous RSI for crossover detection
	var prevRSI decimal.Decimal
	if len(rsi) > 1 {
		prevRSI = decimal.NewFromFloat(rsi[len(rsi)-2])
	}

	// Calculate OBV slope (current vs previous smoothed OBV)
	var obvSlope decimal.Decimal
	if len(obvSmoothed) > 1 {
		current := decimal.NewFromFloat(obvSmoothed[len(obvSmoothed)-1])
		previous := decimal.NewFromFloat(obvSmoothed[len(obvSmoothed)-2])
		obvSlope = current.Sub(previous)
	}

	// Check for recent Bollinger Band lower touch
	touchedLowerRecently := s.checkBBLowerTouch(processedCloses, lower)

	// Verify we have data at this time
	hasDataAtTime, err := d.HasDataAtTime(latest.GetTime())
	if err != nil {
		return nil, err
	}
	if !hasDataAtTime {
		es.SetDirection(order.MissingData)
		es.AppendReasonf("missing data at %v", latest.GetTime())
		return &es, nil
	}

	// Check current position status from portfolio
	hasPosition := false
	
	if es.GetAssetType().IsFutures() {
		// For futures, check positions
		pos, err := p.GetPositions(&es)
		if err == nil {
			for i := range pos {
				if pos[i].Exchange == es.GetExchange() && 
				   pos[i].Asset == es.GetAssetType() && 
				   pos[i].Pair.Equal(es.Pair()) {
					// Check if we have a meaningful position (not just dust)
					if pos[i].LatestSize.GreaterThan(decimal.NewFromFloat(0.00001)) {
						hasPosition = true
						break
					}
				}
			}
		}
	} else {
		// For spot trading, check holdings
		holdings := p.GetLatestHoldingsForAllCurrencies()
		for i := range holdings {
			if holdings[i].Exchange == es.GetExchange() && 
			   holdings[i].Asset == es.GetAssetType() && 
			   holdings[i].Pair.Equal(es.Pair()) {
				// Check if we have meaningful base currency holdings (e.g., SOL)
				// Using a threshold to ignore dust
				if holdings[i].BaseSize.GreaterThan(decimal.NewFromFloat(0.01)) {
					hasPosition = true
					break
				}
			}
		}
	}

	// Generate trading signals based on multi-indicator logic
	if hasPosition {
		// Exit logic for existing positions
		s.evaluateExitConditions(&es, latestRSI, prevRSI, latestClose, latestBBMiddle)
	} else {
		// Entry logic for new positions
		s.evaluateEntryConditions(&es, latestEMAFast, latestEMASlow, latestRSI, prevRSI, 
			latestClose, latestBBMiddle, obvSlope, touchedLowerRecently)
	}

	// Add detailed reasoning with all indicator values
	es.AppendReasonf("Indicators: EMA50=%.2f EMA200=%.2f RSI=%.2f(prev=%.2f) BB_mid=%.2f Close=%.2f OBV_slope=%.4f touched_lower=%t", 
		latestEMAFast.InexactFloat64(), latestEMASlow.InexactFloat64(), 
		latestRSI.InexactFloat64(), prevRSI.InexactFloat64(),
		latestBBMiddle.InexactFloat64(), latestClose.InexactFloat64(),
		obvSlope.InexactFloat64(), touchedLowerRecently)

	return &es, nil
}

// OnSimultaneousSignals analyses multiple data points simultaneously, allowing flexibility
// in allowing a strategy to only place an order for X currency if Y currency's price is Z
func (s *Strategy) OnSimultaneousSignals(d []data.Handler, f funding.IFundingTransferer, p portfolio.Handler) ([]signal.Event, error) {
	var resp []signal.Event
	var errs error
	for i := range d {
		latest, err := d[i].Latest()
		if err != nil {
			return nil, err
		}
		sigEvent, err := s.OnSignal(d[i], f, p)
		if err != nil {
			errs = fmt.Errorf("%v %v %v %w",
				latest.GetExchange(),
				latest.GetAssetType(),
				latest.Pair(),
				err)
		} else {
			resp = append(resp, sigEvent)
		}
	}
	return resp, errs
}

// massageMissingData will replace missing data with the previous data point
// this ensures that indicators can be calculated correctly when there are gaps
func (s *Strategy) massageMissingData(data []decimal.Decimal, t time.Time) ([]float64, error) {
	resp := make([]float64, len(data))
	var missingDataStreak int64
	minPeriod := s.emaSlowPeriod.IntPart() // Use longest period for validation
	
	for i := range data {
		if data[i].IsZero() && i > int(minPeriod) {
			data[i] = data[i-1]
			missingDataStreak++
		} else {
			missingDataStreak = 0
		}
		if missingDataStreak >= minPeriod {
			return nil, fmt.Errorf("missing data exceeds minimum period length of %v at %s and will distort results: %w",
				minPeriod,
				t.Format(time.DateTime),
				base.ErrTooMuchBadData)
		}
		resp[i] = data[i].InexactFloat64()
	}
	return resp, nil
}

// evaluateExitConditions determines when to exit existing positions
func (s *Strategy) evaluateExitConditions(es *signal.Signal, latestRSI, prevRSI, latestClose, latestBBMiddle decimal.Decimal) {
	// Exit condition 1: RSI overbought reversal
	rsiOverboughtReversal := prevRSI.GreaterThanOrEqual(s.rsiExitOB) && latestRSI.LessThan(prevRSI)
	
	// Exit condition 2: Structure breakdown (close below BB middle)
	structureBreakdown := latestClose.LessThan(latestBBMiddle)

	if rsiOverboughtReversal || structureBreakdown {
		es.SetDirection(order.Sell)
		if rsiOverboughtReversal {
			es.AppendReasonf("Exit: RSI overbought reversal (%.2f->%.2f)", 
				prevRSI.InexactFloat64(), latestRSI.InexactFloat64())
		}
		if structureBreakdown {
			es.AppendReasonf("Exit: Close below BB middle (%.2f < %.2f)", 
				latestClose.InexactFloat64(), latestBBMiddle.InexactFloat64())
		}
		// Portfolio manager will handle the exit amount
	} else {
		es.SetDirection(order.DoNothing)
		es.AppendReason("Holding position - no exit signals")
	}
}

// evaluateEntryConditions determines when to enter new positions
func (s *Strategy) evaluateEntryConditions(es *signal.Signal, emaFast, emaSlow, latestRSI, prevRSI, 
	latestClose, latestBBMiddle, obvSlope decimal.Decimal, touchedLowerRecently bool) {
	
	// Relaxed entry conditions - more flexible thresholds
	trendUp := emaFast.GreaterThan(emaSlow) // Keep as regime filter
	
	// Momentum: RSI > 50 OR RSI slope up (instead of strict cross at 40)
	rsiMomentum := latestRSI.GreaterThan(decimal.NewFromInt(50)) || 
		latestRSI.GreaterThan(prevRSI)
	
	// Structure: Allow tolerance around mid-band (mid - 0.25*std)
	bbTolerance := s.bbStdDev.Mul(decimal.NewFromFloat(0.25))
	structureOK := touchedLowerRecently && 
		latestClose.GreaterThanOrEqual(latestBBMiddle.Sub(bbTolerance))
	
	// Volume: OBV slope positive (keep as is for now)
	volumeOK := obvSlope.GreaterThan(decimal.Zero)

	// Log individual condition states for debugging
	var conditionsMet []string
	var conditionsNotMet []string
	var conditionsCount int
	
	// Trend is a mandatory gate condition
	if trendUp {
		conditionsMet = append(conditionsMet, "TREND✓(EMA12>EMA26)")
	} else {
		conditionsNotMet = append(conditionsNotMet, "TREND✗(EMA12<EMA26)")
	}
	
	// Count the 3 flexible conditions
	if rsiMomentum {
		conditionsMet = append(conditionsMet, fmt.Sprintf("MOMENTUM✓(RSI:%.1f)", 
			latestRSI.InexactFloat64()))
		conditionsCount++
	} else {
		conditionsNotMet = append(conditionsNotMet, fmt.Sprintf("MOMENTUM✗(RSI:%.1f)", 
			latestRSI.InexactFloat64()))
	}
	
	if structureOK {
		conditionsMet = append(conditionsMet, "STRUCTURE✓(touched_lower+tolerance)")
		conditionsCount++
	} else {
		if touchedLowerRecently {
			conditionsNotMet = append(conditionsNotMet, fmt.Sprintf("STRUCTURE✗(touched✓,price<tolerance:%.0f)", 
				latestClose.InexactFloat64()))
		} else {
			conditionsNotMet = append(conditionsNotMet, "STRUCTURE✗(no_lower_touch)")
		}
	}
	
	if volumeOK {
		conditionsMet = append(conditionsMet, fmt.Sprintf("VOLUME✓(OBV_slope:%.1f)", 
			obvSlope.InexactFloat64()))
		conditionsCount++
	} else {
		conditionsNotMet = append(conditionsNotMet, fmt.Sprintf("VOLUME✗(OBV_slope:%.1f)", 
			obvSlope.InexactFloat64()))
	}
	
	// Log condition summary (trend + X of 3 flexible)
	es.AppendReasonf("GATE[Trend:%v] SIGNALS[%d/3]: MET[%s] NOT_MET[%s]", 
		trendUp, conditionsCount,
		strings.Join(conditionsMet, ", "),
		strings.Join(conditionsNotMet, ", "))
	
	// Track state changes
	var stateChanges []string
	if trendUp != s.prevTrendUp {
		if trendUp {
			stateChanges = append(stateChanges, "📈TREND_ON")
		} else {
			stateChanges = append(stateChanges, "📉TREND_OFF")
		}
		s.prevTrendUp = trendUp
	}
	
	if rsiMomentum != s.prevRSIMomentum {
		if rsiMomentum {
			stateChanges = append(stateChanges, "⚡MOMENTUM_POSITIVE")
		} else {
			stateChanges = append(stateChanges, "💤MOMENTUM_NEGATIVE")
		}
		s.prevRSIMomentum = rsiMomentum
	}
	
	if structureOK != s.prevStructureOK {
		if structureOK {
			stateChanges = append(stateChanges, "🎯STRUCTURE_VALID")
		} else {
			stateChanges = append(stateChanges, "❌STRUCTURE_INVALID")
		}
		s.prevStructureOK = structureOK
	}
	
	if volumeOK != s.prevVolumeOK {
		if volumeOK {
			stateChanges = append(stateChanges, "📊VOLUME_POSITIVE")
		} else {
			stateChanges = append(stateChanges, "📊VOLUME_NEGATIVE")
		}
		s.prevVolumeOK = volumeOK
	}
	
	if conditionsCount != s.prevConditionCount {
		if conditionsCount > s.prevConditionCount {
			stateChanges = append(stateChanges, fmt.Sprintf("⬆️CONDITIONS_IMPROVED[%d->%d]", 
				s.prevConditionCount, conditionsCount))
		} else {
			stateChanges = append(stateChanges, fmt.Sprintf("⬇️CONDITIONS_DEGRADED[%d->%d]", 
				s.prevConditionCount, conditionsCount))
		}
		s.prevConditionCount = conditionsCount
	}
	
	if len(stateChanges) > 0 {
		es.AppendReasonf("STATE_CHANGES: %s", strings.Join(stateChanges, ", "))
	}

	// Entry logic: Trend gate (mandatory) + at least 2 of 3 flexible conditions
	if trendUp && conditionsCount >= 2 {
		es.SetDirection(order.Buy)
		es.AppendReasonf("🎯 ENTRY SIGNAL: Trend gate passed + %d/3 signals met!", conditionsCount)
		
		// Set a buy limit slightly above current price to help ensure fills in backtesting
		// The actual position sizing will be handled by GCT's portfolio risk manager
		es.SetBuyLimit(latestClose.Mul(decimal.NewFromFloat(1.001)))
		
	} else {
		es.SetDirection(order.DoNothing)
		if !trendUp {
			es.AppendReason("No entry: Trend gate failed (need EMA12>EMA26)")
		} else if conditionsCount < 2 {
			es.AppendReasonf("No entry: Only %d/3 signals met (need 2+)", conditionsCount)
		}
	}
}

// checkBBLowerTouch checks if price touched the lower Bollinger Band in recent periods
func (s *Strategy) checkBBLowerTouch(closes []float64, lower []float64) bool {
	lookback := int(s.lookbackPeriods.IntPart())
	if len(closes) < lookback || len(lower) < lookback {
		return false
	}

	// Check last N periods for a touch of the lower band
	startIdx := len(closes) - lookback
	for i := startIdx; i < len(closes)-1; i++ { // Exclude current period
		if i >= 0 && i < len(lower) {
			closePrice := decimal.NewFromFloat(closes[i])
			lowerBand := decimal.NewFromFloat(lower[i])
			if closePrice.LessThanOrEqual(lowerBand) {
				return true
			}
		}
	}
	return false
}


// SupportsSimultaneousProcessing returns whether the strategy can handle multiple currencies simultaneously  
func (s *Strategy) SupportsSimultaneousProcessing() bool {
	return true
}

// SetCustomSettings allows configuration of strategy parameters via .strat files
func (s *Strategy) SetCustomSettings(customSettings map[string]interface{}) error {
	for k, v := range customSettings {
		switch k {
		case emaFastPeriodKey:
			emaFast, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", emaFastPeriodKey)
			}
			s.emaFastPeriod = decimal.NewFromFloat(emaFast)
		case emaSlowPeriodKey:
			emaSlow, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", emaSlowPeriodKey)
			}
			s.emaSlowPeriod = decimal.NewFromFloat(emaSlow)
		case rsiPeriodKey:
			rsiPer, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", rsiPeriodKey)
			}
			s.rsiPeriod = decimal.NewFromFloat(rsiPer)
		case rsiLongTrigKey:
			rsiTrig, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", rsiLongTrigKey)
			}
			s.rsiLongTrig = decimal.NewFromFloat(rsiTrig)
		case rsiExitOBKey:
			rsiExit, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", rsiExitOBKey)
			}
			s.rsiExitOB = decimal.NewFromFloat(rsiExit)
		case bbPeriodKey:
			bbPer, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", bbPeriodKey)
			}
			s.bbPeriod = decimal.NewFromFloat(bbPer)
		case bbStdDevKey:
			bbStd, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", bbStdDevKey)
			}
			s.bbStdDev = decimal.NewFromFloat(bbStd)
		case obvSmoothKey:
			obvSm, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", obvSmoothKey)
			}
			s.obvSmooth = decimal.NewFromFloat(obvSm)
		case lookbackPeriodsKey:
			lookback, ok := v.(float64)
			if !ok {
				return fmt.Errorf("invalid %s value: expected float64", lookbackPeriodsKey)
			}
			s.lookbackPeriods = decimal.NewFromFloat(lookback)
		default:
			return fmt.Errorf("unknown custom setting: %s", k)
		}
	}
	return nil
}

// SetDefaults sets default values for strategy parameters
func (s *Strategy) SetDefaults() {
	s.emaFastPeriod = decimal.NewFromInt(50)
	s.emaSlowPeriod = decimal.NewFromInt(200)
	s.rsiPeriod = decimal.NewFromInt(14)
	s.rsiLongTrig = decimal.NewFromInt(40)
	s.rsiExitOB = decimal.NewFromInt(70)
	s.bbPeriod = decimal.NewFromInt(20)
	s.bbStdDev = decimal.NewFromFloat(2.0)
	s.obvSmooth = decimal.NewFromInt(10)
	s.lookbackPeriods = decimal.NewFromInt(10)
}