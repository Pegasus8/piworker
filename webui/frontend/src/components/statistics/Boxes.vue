<template>
  <div class="container-fluid my-4">
    <div class="row justify-content-center mx-5 p-4">
      <div
        class="box col-10 col-sm-6 col-md-5 col-lg-3 col-xl-2 border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <div class="card-body text-center">
          <p class="card-title text-muted h4">Active</p>
          <h3
            class="card-subtitle mb-2 text-light display-4"
            id="active-tasks-number"
          >{{ activeTasksCounter }}</h3>
        </div>
      </div>

      <div
        class="box col-10 col-sm-6 col-md-5 col-lg-3 col-xl-2 border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <div class="card-body text-center">
          <p class="card-title text-muted h4">On Execution</p>
          <h3
            class="card-subtitle mb-2 text-light display-4"
            id="onexecution-tasks-number"
          >{{ onExecutionTasksCounter }}</h3>
        </div>
      </div>

      <div
        class="box col-10 col-sm-6 col-md-5 col-lg-3 col-xl-2 border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <div class="card-body text-center">
          <p class="card-title text-muted h4">Inactive</p>
          <h3
            class="card-subtitle mb-2 text-light display-4"
            id="inactive-tasks-number"
          >{{ inactiveTasksCounter }}</h3>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import { mapGetters } from 'vuex'

export default {
  computed: {
    ...mapGetters('statistics', [
      'activeTasksCounter',
      'onExecutionTasksCounter',
      'inactiveTasksCounter'
    ])
  }
}
</script>

<style lang="scss" scoped>
</style>
