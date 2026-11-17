package service

import "cadenceforge/internal/domain"

func (s *Service) SaveRecipe(id string, expected int, input domain.Recipe) (domain.Recipe, error) {
	if err := domain.ValidateRecipe(input); err != nil {
		return domain.Recipe{}, err
	}
	s.mutation.Lock()
	defer s.mutation.Unlock()
	if id == "" {
		id = ID()
	}
	var out domain.Recipe
	err := s.Store.Update(func(st *domain.State) error {
		history := st.RecipeHistory(id)
		if expected > 0 && history == nil {
			return domain.Missing("recipe", id)
		}
		if history == nil {
			history = domain.NewHistory[domain.Recipe]()
			st.Recipes[id] = history
		}
		input.ID = id
		// Recipe validity remains recipe-domain knowledge; the shared history
		// supplies only revision numbering, optimistic conflict and freezing.
		committed, err := history.Append(expected, input, domain.ValidateRecipe)
		if err != nil {
			return err
		}
		out = committed
		st.Emit(id, "recipe.revised", input.Format)
		return nil
	})
	return out, err
}
