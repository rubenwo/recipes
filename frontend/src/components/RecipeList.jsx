import { Link } from 'react-router-dom';
import RecipeCard from './RecipeCard';

export default function RecipeList({ recipes, onDelete, searchQuery = '' }) {
  if (recipes.length === 0) {
    const generateHref = searchQuery.trim()
      ? `/generate?notes=${encodeURIComponent(searchQuery.trim())}`
      : '/generate';
    return (
      <p className="empty-state">
        No recipes found. <Link to={generateHref}>Generate some!</Link>
      </p>
    );
  }

  return (
    <div className="recipe-list">
      {recipes.map(recipe => (
        <RecipeCard key={recipe.id} recipe={recipe} showLink onDelete={onDelete} />
      ))}
    </div>
  );
}
