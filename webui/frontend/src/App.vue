<template>
  <v-app>
    <app-navigation-drawer
      :expanded="expandNavDrawer"
    />
    <app-navbar
      @expandDrawer='expandNavDrawer = !expandNavDrawer'
    />
    <v-content>
      <transition
        name="fade"
        mode="out-in"
      >
        <router-view style="animation-duration: 0.3s"/>
      </transition>
    </v-content>
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
@import url("assets/css/icomoon.css");

.floating-alert {
  position: fixed;
  bottom: 2%;
  right: 2%;
}

</style>
