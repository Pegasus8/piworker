<template>
  <v-app-bar elevate-on-scroll dark dense app>
    <v-fade-transition>
      <v-app-bar-nav-icon
        v-show="$store.getters['auth/isAuthenticated']"
        @click.stop="expandNavDrawer"
      />
    </v-fade-transition>
     <v-toolbar-title class="font-weight-bold" >
        PiWorker <span class="overline">v0.1.0-alpha</span>
      </v-toolbar-title>
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
