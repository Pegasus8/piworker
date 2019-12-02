<template>
  <div id="app">
    <app-navbar/>
    <transition
      name="fade"
      mode="out-in"
    >
      <router-view style="animation-duration: 0.3s"/>
    </transition>
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
@import url("assets/css/icomoon.css");

#app {
  font-family: 'Avenir', Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-align: center;
}

.floating-alert {
  position: fixed;
  bottom: 2%;
  right: 2%;
}

</style>
