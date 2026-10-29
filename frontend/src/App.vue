<script setup>
import { onMounted, ref } from 'vue';
import { apiRequest } from './api.js';
import { keycloak } from './keycloak.js';

const authenticated = ref(false);
const loading = ref(true);
const saving = ref(false);
const targetUrl = ref('');
const links = ref([]);
const selectedStats = ref(null);
const errorMessage = ref('');
const notice = ref('');
const username = ref('');

async function getToken() {
  await keycloak.updateToken(30);
  return keycloak.token;
}

async function loadLinks() {
  errorMessage.value = '';
  try {
    links.value = await apiRequest('/api/links', { getToken });
  } catch (error) {
    errorMessage.value = error.message;
  }
}

async function createLink() {
  saving.value = true;
  errorMessage.value = '';
  notice.value = '';
  try {
    await apiRequest('/api/links', {
      method: 'POST',
      getToken,
      body: { targetUrl: targetUrl.value.trim() },
    });
    targetUrl.value = '';
    notice.value = 'Link criado.';
    await loadLinks();
  } catch (error) {
    errorMessage.value = error.message;
  } finally {
    saving.value = false;
  }
}

async function deleteLink(link) {
  errorMessage.value = '';
  try {
    await apiRequest(`/api/links/${encodeURIComponent(link.id)}`, {
      method: 'DELETE',
      getToken,
    });
    links.value = links.value.filter((item) => item.id !== link.id);
    if (selectedStats.value?.linkId === link.id) selectedStats.value = null;
    notice.value = 'Link removido.';
  } catch (error) {
    errorMessage.value = error.message;
  }
}

async function toggleStats(link) {
  if (selectedStats.value?.linkId === link.id) {
    selectedStats.value = null;
    return;
  }
  errorMessage.value = '';
  try {
    selectedStats.value = await apiRequest(`/api/links/${encodeURIComponent(link.id)}/stats`, {
      getToken,
    });
  } catch (error) {
    errorMessage.value = error.message;
  }
}

async function copyLink(url) {
  try {
    await navigator.clipboard.writeText(url);
    notice.value = 'URL copiada.';
  } catch {
    errorMessage.value = 'Não foi possível copiar a URL neste navegador.';
  }
}

async function login() {
  await keycloak.login({ redirectUri: window.location.origin });
}

async function logout() {
  await keycloak.logout({ redirectUri: window.location.origin });
}

onMounted(async () => {
  try {
    authenticated.value = await keycloak.init({
      onLoad: 'check-sso',
      pkceMethod: 'S256',
      checkLoginIframe: false,
    });
    if (authenticated.value) {
      username.value = keycloak.tokenParsed?.preferred_username ?? 'Conta';
      await loadLinks();
    }
  } catch {
    errorMessage.value = 'Não foi possível conectar ao serviço de autenticação.';
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="/" aria-label="EncurtadorURL início">
        <span class="brand-mark" aria-hidden="true">E</span>
        <span>EncurtadorURL</span>
      </a>
      <div v-if="authenticated" class="account">
        <span class="account-name">{{ username }}</span>
        <button class="button button-quiet" type="button" @click="logout">Sair</button>
      </div>
    </header>

    <main class="main-content">
      <p v-if="loading" class="loading-state">Conectando...</p>

      <section v-else-if="!authenticated" class="welcome">
        <div class="welcome-copy">
          <p class="eyebrow">SEUS LINKS, SEM COMPLICAÇÃO</p>
          <h1>Encurte. Compartilhe. Acompanhe.</h1>
          <p class="intro">Entre para criar links curtos e acompanhar os acessos.</p>
          <button class="button button-primary" type="button" @click="login">
            Entrar com EncurtadorURL
          </button>
        </div>
        <div class="welcome-art" aria-hidden="true">
          <span class="art-link art-link-one">https://site.com/artigo/longo</span>
          <span class="art-arrow">↓</span>
          <span class="art-link art-link-two">EncurtadorURL/r/aB3xK9q</span>
        </div>
      </section>

      <section v-else class="workspace">
        <div class="page-heading">
          <div>
            <p class="eyebrow">PAINEL DE LINKS</p>
            <h1>Seus links, em ordem.</h1>
          </div>
          <span class="link-count">{{ links.length }} {{ links.length === 1 ? 'link' : 'links' }}</span>
        </div>

        <form class="create-form" @submit.prevent="createLink">
          <label for="target-url">URL de destino</label>
          <div class="form-row">
            <input
              id="target-url"
              v-model="targetUrl"
              type="url"
              placeholder="https://exemplo.com/sua-pagina"
              maxlength="2048"
              required
            />
            <button class="button button-primary" type="submit" :disabled="saving">
              {{ saving ? 'Criando...' : 'Criar link' }}
            </button>
          </div>
        </form>

        <p v-if="errorMessage" class="feedback feedback-error" role="alert">{{ errorMessage }}</p>
        <p v-if="notice" class="feedback feedback-notice" role="status">{{ notice }}</p>

        <section class="links-section" aria-labelledby="links-title">
          <div class="section-heading">
            <h2 id="links-title">Links recentes</h2>
            <button class="button button-quiet" type="button" @click="loadLinks">Atualizar</button>
          </div>

          <div v-if="links.length === 0" class="empty-state">
            <p class="empty-mark" aria-hidden="true">↗</p>
            <h3>Nenhum link por aqui</h3>
            <p>Adicione uma URL acima para criar seu primeiro link curto.</p>
          </div>

          <article v-for="link in links" :key="link.id" class="link-item">
            <div class="link-main">
              <a class="short-url" :href="link.shortUrl" target="_blank" rel="noreferrer">
                {{ link.shortUrl }}
              </a>
              <p class="target-url">{{ link.targetUrl }}</p>
            </div>
            <div class="link-metrics">
              <strong>{{ link.totalClicks ?? '—' }}</strong>
              <span>acessos</span>
            </div>
            <div class="link-actions">
              <button class="button button-quiet" type="button" @click="copyLink(link.shortUrl)">
                Copiar
              </button>
              <button class="button button-quiet" type="button" @click="toggleStats(link)">
                {{ selectedStats?.linkId === link.id ? 'Fechar dados' : 'Estatísticas' }}
              </button>
              <button class="button button-danger" type="button" @click="deleteLink(link)">
                Excluir
              </button>
            </div>
            <div v-if="selectedStats?.linkId === link.id" class="stats-detail">
              <div class="stats-total">
                <span>Total de acessos</span>
                <strong>{{ selectedStats.totalClicks }}</strong>
              </div>
              <ul v-if="selectedStats.clicksPerDay.length" class="daily-list">
                <li v-for="day in selectedStats.clicksPerDay" :key="day.date">
                  <time :datetime="day.date">{{ day.date }}</time>
                  <span>{{ day.clicks }} acessos</span>
                </li>
              </ul>
              <p v-else class="no-clicks">Este link ainda não recebeu acessos.</p>
            </div>
          </article>
        </section>
      </section>
    </main>
  </div>
</template>