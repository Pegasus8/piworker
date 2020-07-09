<template>
  <v-app>
    <app-navigation-drawer
      v-if="$store.getters['auth/isAuthenticated']"
      :expanded="expandNavDrawer"
      @stateChange='expandNavDrawer = $event'
    />
    <app-navbar
      @expandDrawer='expandNavDrawer = !expandNavDrawer'
    />
    <v-main>
      <v-fade-transition
        mode="out-in"
      >
        <router-view style="animation-duration: 0.3s"/>
      </v-fade-transition>
    </v-main>
  </v-app>
</template>

<script>
import Navbar from './components/shared/navbar/Navbar.vue'
import NavigationDrawer from './components/shared/navigation-drawer/NavigationDrawer.vue'

export default {
  data () {
    return {
      expandNavDrawer: false
    }
  },
  methods: {
  },
  components: {
    appNavbar: Navbar,
    appNavigationDrawer: NavigationDrawer
  }
}
</script>

<style lang="scss">
</style>
