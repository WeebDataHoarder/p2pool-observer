package views

import (
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"math"
	"slices"
	"strconv"
	"time"
)

type PositionGraphDivider struct {
	StartValue float64
	EndValue   float64
	Label      string
	Color      string
	Border     string
}

type PositionGraphPoint struct {
	X, Y               float64
	Link, Color, Label string
	Square             bool
}

const DefaultBlocksPositionChartDuration = time.Hour * 8
const DefaultSharesPositionChartDuration = time.Hour * 12

func NewBlocksPositionChart(ctx *GlobalRequestContext, foundBlocks []*index.FoundBlock, x time.Duration) (dividersX, dividersY []PositionGraphDivider, points []PositionGraphPoint) {

	current := time.Unix(int64(ctx.Pool.SideChain.LastBlock.Timestamp), 0).UTC()

	secondsPerBlock := time.Second * time.Duration(ctx.Consensus.TargetBlockTime)

	xSeconds := x.Seconds()
	var highestEffort float64

	for i, b := range foundBlocks {
		var p PositionGraphPoint
		var effortNumber float64
		if len(foundBlocks) > (i + 1) {
			effortNumber = found_block_effort(b, foundBlocks[i+1])
		} else if effortIndex := slices.IndexFunc(ctx.Pool.SideChain.Effort.Last, func(e cmdutils.PoolInfoResultSideChainEffortLastEntry) bool { return e.Id == b.MainBlock.Id }); effortIndex != -1 {
			effortNumber = ctx.Pool.SideChain.Effort.Last[effortIndex].Effort
		}

		p.Label = fmt.Sprintf("Block %d %s", b.MainBlock.Height, time_elapsed_long(b.MainBlock.Timestamp))
		p.X = (xSeconds - current.Sub(time.Unix(int64(b.MainBlock.Timestamp), 0).UTC()).Seconds()) / xSeconds
		if p.X < 0 {
			//do not include if it is over chart
			continue
		}

		p.Y = effortNumber
		if effortNumber > highestEffort {
			highestEffort = effortNumber
		}

		if effortNumber > 0.0 {
			p.Label += fmt.Sprintf(" / %.2f%%", effortNumber)
			p.Color = effort_color(effortNumber)
		}

		p.Link = "/share/" + hex(ctx, b.MainBlock.SideTemplateId)

		points = append(points, p)
	}

	highestEffortInt := int(math.Ceil(highestEffort / 100))
	highestEffort = float64(highestEffortInt * 100)
	//adjust y
	for i := range points {
		points[i].Y = (highestEffort - points[i].Y) / highestEffort
	}

	for i := highestEffortInt; i > 0; i-- {
		dividersY = append(dividersY, PositionGraphDivider{
			StartValue: float64(i*100) / highestEffort,
			EndValue:   float64((i-1)*100) / highestEffort,
			Label:      strconv.FormatUint(uint64(i*100), 10) + "%",
		})
	}

	dividersX = append(dividersX, PositionGraphDivider{
		StartValue: (time.Duration(ctx.Pool.SideChain.Window.Blocks) * secondsPerBlock).Seconds() / xSeconds,
		EndValue:   0,
		Label:      "PPLNS",
		Color:      "rgba(255, 102, 0, 0.5)",
		Border:     "unset",
	})

	return dividersX, dividersY, points
}

func NewSharesPositionChart(ctx *GlobalRequestContext, shares []*index.SideBlock, payouts *[]*index.Payout, efforts []float64, x time.Duration) (dividersX, dividersY []PositionGraphDivider, points []PositionGraphPoint) {

	current := time.Unix(int64(ctx.Pool.SideChain.LastBlock.Timestamp), 0).UTC()

	secondsPerBlock := time.Second * time.Duration(ctx.Consensus.TargetBlockTime)

	xSeconds := x.Seconds()
	var highestEffort float64

	for i, s := range shares {
		var p PositionGraphPoint
		var effortNumber float64
		if efforts != nil {
			if effort := efforts[i]; effort >= 0 {
				effortNumber = effort
			}
		}

		p.Label = fmt.Sprintf("Share %d / %s", s.SideHeight, time_elapsed_long(s.Timestamp))
		p.X = (xSeconds - current.Sub(time.Unix(int64(s.Timestamp), 0).UTC()).Seconds()) / xSeconds
		if p.X < 0 {
			//do not include if it is over chart
			continue
		}

		//todo: log chart?

		//cap unlikely odds from chart
		var effortPosition = effortNumber
		if effortPosition > 600 {
			effortPosition = 600
		}

		p.Y = effortPosition
		if effortPosition > highestEffort {
			highestEffort = effortPosition
		}

		if effortNumber > 0.0 {
			p.Label += fmt.Sprintf(" / %.2f%%", effortNumber)
			p.Color = effort_color(effortNumber)
			if effortPosition < effortNumber {
				//max, display darker
				p.Color = "#222222"
			}
		}

		p.Link = "/share/" + hex(ctx, s.TemplateId)

		if s.IsUncle() {
			p.Label += " (uncle)"
			p.Square = true
		}

		points = append(points, p)
	}

	highestEffortInt := max(3, int(math.Ceil(highestEffort/100)))
	highestEffort = float64(highestEffortInt * 100)
	//adjust y
	for i := range points {
		points[i].Y = (highestEffort - points[i].Y) / highestEffort
	}

	for i := highestEffortInt; i > 0; i-- {
		dividersY = append(dividersY, PositionGraphDivider{
			StartValue: float64(i*100) / highestEffort,
			EndValue:   float64((i-1)*100) / highestEffort,
			Label:      strconv.FormatUint(uint64(i*100), 10) + "%",
		})
	}

	dividersX = append(dividersX, PositionGraphDivider{
		StartValue: (time.Duration(ctx.Pool.SideChain.Window.Blocks) * secondsPerBlock).Seconds() / xSeconds,
		EndValue:   0,
		Label:      "PPLNS",
		Color:      "rgba(255, 102, 0, 0.5)",
		Border:     "unset",
	})

	if payouts != nil {
		for _, p := range *payouts {
			x := (xSeconds - current.Sub(time.Unix(int64(p.Timestamp), 0).UTC()).Seconds()) / xSeconds
			if x < 0 {
				//do not include if it is over chart
				continue
			}
			dividersX = append(dividersX, PositionGraphDivider{
				StartValue: 1 - x,
				EndValue:   1 - x,
			})
		}
	}

	return dividersX, dividersY, points
}
