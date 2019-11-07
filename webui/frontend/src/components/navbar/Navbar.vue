<template>
  <b-navbar toggleable="lg" variant="dark" type="dark" class="text-light">
    <b-navbar-brand class="mb-0 h1 font-weight-bold" href="#">
      PiWorker
      <span class="badge badge-secondary badge-light badge-pill" id="version">Alpha-v0.1.0</span>
    </b-navbar-brand>
    <b-navbar-toggle
      target="navbarSupportedContent"
      aria-label="Toggle navigation"
      v-if="$store.getters['auth/isAuthenticated']"
    />

    <b-collapse id="navbarSupportedContent" is-nav>
      <b-navbar-nav class="mr-auto">
        <router-link
          v-if="$store.getters['auth/isAuthenticated']"
          :to="{name: 'statistics'}"
          class="nav-item"
          active-class="active"
          tag="li"
        >
          <a class="nav-link">Statistics</a>
        </router-link>
        <router-link
          v-if="$store.getters['auth/isAuthenticated']"
          :to="{name: 'management'}"
          class="nav-item"
          active-class="active"
          tag="li"
        >
          <a class="nav-link">Management</a>
        </router-link>
        <router-link
          v-if="$store.getters['auth/isAuthenticated']"
          :to="{name: 'settings'}"
          class="nav-item"
          active-class="active"
          tag="li"
        >
          <a class="nav-link">Settings</a>
        </router-link>
        <li v-if="$store.getters['auth/isAuthenticated']" class="nav-item ml-lg-3">
          <router-link
            type="button"
            class="btn btn-primary my-2 my-lg-0"
            id="newTaskBtn"
            tag="button"
            :to="{name: 'new-task'}"
          >
            New
          </router-link>
        </li>
      </b-navbar-nav>
      <div v-if="$store.getters['auth/isAuthenticated']" class="nav-item">
        <b-button 
          variant="secondary"
          class="my-2 my-lg-0"
          @click="$store.dispatch('auth/logout')"
        >Logout</b-button>
      </div>
    </b-collapse>
  </b-navbar>
</template>

<script>
export default {}
</script>

<style lang="scss" scoped>
</style>
