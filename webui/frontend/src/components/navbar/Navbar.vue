<template>
  <nav class="navbar navbar-expand-lg navbar navbar-dark bg-dark">
    <a class="navbar-brand mb-0 h1 font-weight-bold" href="#">
      PiWorker
      <span class="badge badge-secondary badge-light badge-pill" id="version">Alpha-v0.1.0</span>
    </a>
    <button
      class="navbar-toggler"
      type="button"
      data-toggle="collapse"
      href="#navbarSupportedContent"
      aria-controls="navbarSupportedContent"
      aria-expanded="false"
      aria-label="Toggle navigation"
      v-if="$store.getters['auth/isAuthenticated']"
    >
      <span class="navbar-toggler-icon"></span>
    </button>

    <div class="collapse navbar-collapse" id="navbarSupportedContent">
      <ul class="navbar-nav mr-auto text-center">
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
          <button
            type="button"
            data-toggle="modal"
            data-target="#newTaskModal"
            class="btn btn-primary my-2 my-lg-0"
            id="newTaskBtn"
          >New</button>
        </li>
      </ul>
      <div v-if="$store.getters['auth/isAuthenticated']" class="nav-item">
        <button 
          type="button"
          class="btn btn-secondary my-2 my-lg-0"
          @click="$store.dispatch('auth/logout')"
        >Logout</button>
      </div>
    </div>
  </nav>
</template>

<script>
export default {}
</script>

<style lang="scss" scoped>
</style>
