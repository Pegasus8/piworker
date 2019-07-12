<template>
  <div id="app">
    <app-navbar/>
    <router-view/>
    <app-new-task-modal/>
  </div>
</template>

<script>
import Navbar from './components/navbar/Navbar.vue'
import NewTaskModal from './components/NewTask.vue'

export default {
  components: {
    appNavbar: Navbar,
    appNewTaskModal: NewTaskModal
  }
}
</script>

<style lang="scss">
#app {
  font-family: 'Avenir', Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-align: center;
}

</style>
