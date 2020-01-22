<template>
  <v-app-bar elevate-on-scroll dark dense app>
    <v-app-bar-nav-icon @click="expandNavDrawer"/>
    <v-toolbar-title class="font-weight-bold">
      PiWorker <span class="overline">v0.1.0-alpha</span>
    </v-toolbar-title>
    <!-- <b-navbar-toggle
      target="navbarSupportedContent"
      aria-label="Toggle navigation"
      v-if="$store.getters['auth/isAuthenticated']"
    /> -->

    <!-- <b-collapse id="navbarSupportedContent" is-nav>
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
        <router-link
          v-if="$store.getters['auth/isAuthenticated']"
          :to="{name: 'new-task'}"
          class="nav-item"
          active-class="active"
          tag="li"
        >
          <a class="nav-link font-weight-bold">New Task</a>
        </router-link>
      </b-navbar-nav>
      <div v-if="$store.getters['auth/isAuthenticated']" class="nav-item">
        <b-button
          variant="secondary"
          class="my-2 my-lg-0"
          @click="$store.dispatch('auth/logout')"
        >Logout</b-button>
      </div>
    </b-collapse> -->
  </v-app-bar>
</template>

<script>
export default {
  methods: {
    expandNavDrawer () {
      this.$emit('expandDrawer')
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
