from fastapi.testclient import TestClient

from app.main import create_app

client = TestClient(create_app(start_consumer=False))


def test_health_returns_the_service_status():
    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "service": "stats-service"}
