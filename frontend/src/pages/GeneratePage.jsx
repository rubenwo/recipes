import { useSearchParams } from 'react-router-dom';
import GenerateForm from '../components/GenerateForm';
import GenerationProgress from '../components/GenerationProgress';
import ReviewPanel from '../components/ReviewPanel';
import { useGeneration } from '../hooks/useGeneration';

export default function GeneratePage() {
  const [searchParams] = useSearchParams();
  const initialNotes = searchParams.get('notes') || '';
  const { events, recipes, loading, error, generate, reset, removeRecipe } = useGeneration();

  const handleGenerate = (endpoint, body) => {
    reset();
    generate(endpoint, body);
  };

  const handleRefine = (recipe, feedback) => {
    generate('refine', { recipe, feedback });
  };

  const progressEvents = events.filter(e => e.type !== 'recipe');

  return (
    <div className="generate-page">
      <h2>Generate Recipes</h2>
      <div className="generate-layout">
        <div className="generate-left">
          <GenerateForm onGenerate={handleGenerate} loading={loading} initialNotes={initialNotes} />
        </div>
        <div className="generate-right">
          {error && <div className="error-message">{error}</div>}
          <GenerationProgress events={progressEvents} loading={loading} hasRecipes={recipes.length > 0} />
          <ReviewPanel recipes={recipes} onRefine={handleRefine} onRemove={removeRecipe} loading={loading} />
        </div>
      </div>
    </div>
  );
}
