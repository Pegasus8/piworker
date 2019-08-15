<template>
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
        <li class="nav-item dropdown">
          <a  
            class="nav-link dropdown-toggle" 
            id="managementDropdown"
            role="button"
            data-toggle='dropdown'
            aria-haspopup="true"
            aria-expanded="false"
            href="#">
            Management
          </a>
          <div class="dropdown-menu" aria-labelledby="managementDropdown">
            <router-link class="dropdown-item" :to="{ name: 'tasks-list' }">Tasks</router-link>
            <router-link class="dropdown-item" :to="{ name: 'new-task' }">New Task</router-link>
          </div>
        </li>
        <li class="nav-item">
          <router-link :to="{name: 'settings'}" class="nav-link">Settings</router-link>
        </li>
        <li class="nav-item navbar-right ml-lg-3">
          <button type="button" data-toggle="modal" data-target="#newTaskModal" class="btn btn-primary" id="newTaskBtn">New</button>
        </li>
      </ul>
    </div>
  </nav>
</template>

<script>
export default {
}
</script>

<style lang="scss" scoped>
</style>
