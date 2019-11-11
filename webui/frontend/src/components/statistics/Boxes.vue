<template>
  <b-container class="my-4" fluid>
    <b-row class="justify-content-center mx-5 p-4">
      <b-col
        cols="10"
        sm="6"
        md="5"
        lg="3"
        xl="2"
        class="box border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <b-card-body class="text-center">
          <b-card-title class="text-muted h4">Active</b-card-title>
          <b-card-sub-title>
            <h3
              class="mb-2 text-light display-4"
              id="active-tasks-number"
            >{{ activeTasksCounter }}</h3>
          </b-card-sub-title>
        </b-card-body>
      </b-col>

      <b-col
        cols="10"
        sm="6"
        md="5"
        lg="3"
        xl="2"
        class="box border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <b-card-body class="text-center">
          <b-card-title class="text-muted h4">On Execution</b-card-title>
          <b-card-sub-title>
            <h3
              class="mb-2 text-light display-4"
              id="onexecution-tasks-number"
            >{{ onExecutionTasksCounter }}</h3>
          </b-card-sub-title>
        </b-card-body>
      </b-col>

      <b-col
        cols="10"
        sm="6"
        md="5"
        lg="3"
        xl="2"
        class="box border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
      >
        <b-card-body class="text-center">
          <b-card-title class="text-muted h4">Inactive</b-card-title>
          <b-card-sub-title>
            <h3
              class="mb-2 text-light display-4"
              id="inactive-tasks-number"
            >{{ inactiveTasksCounter }}</h3>
          </b-card-sub-title>
        </b-card-body>
      </b-col>
    </b-row>
  </b-container>
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
