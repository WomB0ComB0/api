import asyncio
import logging
import os
from datetime import datetime

from aiohttp import ClientSession, ClientTimeout, web

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Parse services from environment
SERVICES = {}
if services_env := os.getenv('SERVICES'):
    for service in services_env.split(','):
        name, endpoint = service.split(':')
        SERVICES[name] = f'http://{service}'

async def check_service_health(session: ClientSession, name: str, url: str) -> dict:
    """Check health of a single service"""
    try:
        timeout = ClientTimeout(total=5)
        async with session.get(f'{url}/health', timeout=timeout) as response:
            if response.status == 200:
                return {'service': name, 'status': 'healthy', 'url': url}
            else:
                return {'service': name, 'status': 'unhealthy', 'url': url, 'error': f'Status code: {response.status}'}
    except Exception as e:
        return {'service': name, 'status': 'unhealthy', 'url': url, 'error': str(e)}

async def health_handler(request):
    """Aggregate health check for all services"""
    async with ClientSession() as session:
        tasks = [check_service_health(session, name, url) for name, url in SERVICES.items()]
        results = await asyncio.gather(*tasks)
    
    all_healthy = all(r['status'] == 'healthy' for r in results)
    status_code = 200 if all_healthy else 503
    
    response = {
        'status': 'healthy' if all_healthy else 'degraded',
        'timestamp': datetime.utcnow().isoformat(),
        'services': results
    }
    
    return web.json_response(response, status=status_code)

async def init_app():
    app = web.Application()
    app.router.add_get('/health', health_handler)
    return app

if __name__ == '__main__':
    logger.info(f'🚀 Health aggregator starting with services: {list(SERVICES.keys())}')
    web.run_app(init_app(), host='0.0.0.0', port=8000)
