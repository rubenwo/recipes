-- Consolidate Malay/Nyonya cuisine variants into the canonical "Malaysian" label.
UPDATE recipes
SET cuisine_type = 'Malaysian'
WHERE LOWER(TRIM(cuisine_type)) IN (
    'malay',
    'malaysian',
    'nyonya',
    'nonya',
    'peranakan',
    'nyonya peranakan',
    'nonya peranakan',
    'malay/nyonya'
);

UPDATE pending_recipes
SET cuisine_type = 'Malaysian'
WHERE LOWER(TRIM(cuisine_type)) IN (
    'malay',
    'malaysian',
    'nyonya',
    'nonya',
    'peranakan',
    'nyonya peranakan',
    'nonya peranakan',
    'malay/nyonya'
);
