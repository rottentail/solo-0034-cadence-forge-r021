package service

import (
	"cadenceforge/internal/caption"
	"cadenceforge/internal/domain"
	"cadenceforge/internal/timeline"
	"time"
)

func (s *Service) SaveSheet(id string, expected int, input domain.Sheet) (domain.Sheet, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	if id == "" {
		id = ID()
	}
	var out domain.Sheet
	err := s.Store.Update(func(st *domain.State) error {
		history := st.SheetHistory(id)
		if expected == 0 && history != nil {
			return domain.Conflict("sheet already exists")
		}
		if expected > 0 && history == nil {
			return domain.Missing("sheet", id)
		}
		if history == nil {
			history = domain.NewHistory[domain.Sheet]()
			st.Sheets[id] = history
		}
		input.ID = id
		input.CreatedAt = time.Now().UTC()
		// Validity is domain-specific (source existence, interval bounds); the
		// generic history only runs it inside the optimistic revision protocol.
		committed, err := history.Append(expected, input, func(candidate domain.Sheet) error {
			return domain.ValidateSheet(candidate, st.Sources)
		})
		if err != nil {
			return err
		}
		out = committed
		st.Emit(id, "sheet.revised", "immutable timeline revision saved")
		return nil
	})
	return out, err
}
func (s *Service) Preview(sheetID, recipeID string) (domain.Plan, error) {
	st, err := s.Store.Snapshot()
	if err != nil {
		return domain.Plan{}, err
	}
	sheet, ok := st.SheetHistory(sheetID).Latest()
	if !ok {
		return domain.Plan{}, domain.Missing("sheet", sheetID)
	}
	recipe, ok := st.RecipeHistory(recipeID).Latest()
	if !ok {
		return domain.Plan{}, domain.Missing("recipe", recipeID)
	}
	plan, err := timeline.Build(sheet, recipe, st.Sources)
	if err != nil {
		return plan, err
	}
	plan.VTT = caption.VTT(caption.Map(sheet, plan))
	return plan, nil
}
