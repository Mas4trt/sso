CREATE TABLE IF NOT EXISTS apps (
    id     BIGSERIAL PRIMARY KEY,
    name   TEXT NOT NULL UNIQUE,
    secret TEXT NOT NULL
);

INSERT INTO apps (id, name, secret) VALUES
    (1, 'sso-test-app', 'test-secret-change-me')
ON CONFLICT (id) DO NOTHING;

-- Явный INSERT с id=1 не продвигает apps_id_seq — без этого следующий
-- INSERT без указания id снова попытается взять nextval()=1 и упадёт
-- на unique violation. setval подтягивает sequence к максимальному id.
SELECT setval('apps_id_seq', (SELECT MAX(id) FROM apps));