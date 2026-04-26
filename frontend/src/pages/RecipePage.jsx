import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getRecipe, deleteRecipe } from '../api/client';
import RecipeDetail from '../components/RecipeDetail';

export default function RecipePage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [recipe, setRecipe] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    getRecipe(id)
      .then(setRecipe)
      .catch(err => setError(err.message))
      .finally(() => setLoading(false));
  }, [id]);

  const handleDelete = async () => {
    if (!confirm('Delete this recipe?')) return;
    try {
      await deleteRecipe(id);
      navigate('/library');
    } catch (err) {
      setError(err.message);
    }
  };

  if (loading) return <p>Loading...</p>;
  if (error) return <div className="error-message">{error}</div>;
  if (!recipe) return <p>Recipe not found.</p>;

  return (
    <div className="recipe-detail-page">
      <RecipeDetail recipe={recipe} />
      <div className="recipe-page-actions" style={{ padding: '0 24px', marginTop: 32 }}>
        <button className="btn btn-secondary" onClick={() => navigate('/library')}>← Back to library</button>
        <button className="btn btn-danger" onClick={handleDelete} style={{ marginLeft: 'auto' }}>Delete recipe</button>
      </div>
    </div>
  );
}
