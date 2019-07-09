<template>
  <div id="app">
    <app-navbar></app-navbar>
    <router-view/>
  </div>
</template>

<script>
import Navbar from './components/navbar/Navbar.vue'

export default {
  components: {
    appNavbar: Navbar
  }
}
</script>

<style lang="scss">
#app {
  font-family: 'Avenir', Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-align: center;
  background-color: #1e1e1e;
  // min-height: 100%;
  height: 100vh;
}

</style>
