<template>
  <header>
    <nav class="navbar navbar-expand-lg navbar navbar-dark bg-dark">
      <a class="navbar-brand mb-0 h1" href="#">
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
      >
        <span class="navbar-toggler-icon"></span>
      </button>

      <div class="collapse navbar-collapse" id="navbarSupportedContent">
        <ul class="navbar-nav mr-auto text-center">
          <li class="nav-item active">
            <router-link :to="{name: 'statistics'}" class="nav-link">Statistics</router-link>
          </li>
          <li class="nav-item">
            <router-link :to="{name: 'management'}" class="nav-link">Management</router-link>
          </li>
          <li class="nav-item">
            <router-link :to="{name: 'settings'}" class="nav-link">Settings</router-link>
          </li>
        </ul>
      </div>
    </nav>
  </header>
</template>

<script>
export default {
  method: {
    onItemClick (event) {
      console.log(event)
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
