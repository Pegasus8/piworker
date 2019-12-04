<template>
  <b-container class="my-4" fluid>
    <b-row class="justify-content-center mx-5 p-4">
      <b-col
        cols="10"
        sm="6"
        md="5"
        lg="3"
        xl="2"
        class="border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
        id="atcBox"
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
        class="border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
        id="oetcBox"
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
        class="border border-secondary card m-3 mx-md-4 mx-lg-5 py-4 bg-dark"
        id="itcBox"
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
import anime from 'animejs'

export default {
  data () {
    return {
      atcAnimation: null,
      oetcAnimation: null,
      itcAnimation: null,
      atc: 0,
      oetc: 0,
      itc: 0
    }
  },
  computed: {
    activeTasksCounter () {
      if (this.atc != this.$store.getters['statistics/activeTasksCounter']) {
        if (this.atcAnimation != null) {
            this.atcAnimation.restart()
        } else {
          this.atcAnimation = this.boxAnimation('atcBox')
        }
        this.atc = this.$store.getters['statistics/activeTasksCounter']
      }

      return this.$store.getters['statistics/activeTasksCounter']
    },
    onExecutionTasksCounter () {
      if (this.oetc != this.$store.getters['statistics/onExecutionTasksCounter']) {
        if (this.oetcAnimation != null) {
          this.oetcAnimation.restart()
        } else {
          this.oetcAnimation = this.boxAnimation('oetcBox')
        }
        this.oetc = this.$store.getters['statistics/onExecutionTasksCounter']
      }

      return this.$store.getters['statistics/onExecutionTasksCounter']
    },
    inactiveTasksCounter () {
      if (this.itc != this.$store.getters['statistics/inactiveTasksCounter']) {
        if (this.itcAnimation != null) {
          this.itcAnimation.restart()
        } else {
          this.itcAnimation = this.boxAnimation('itcBox')
        }
        this.itc = this.$store.getters['statistics/inactiveTasksCounter']
      }

      return this.$store.getters['statistics/inactiveTasksCounter']
    }
  },
  methods: {
    boxAnimation (idTarget) {
      const blue = '48, 170, 232'

      let timeline = anime.timeline({ easing: 'linear', direction: 'alternate' })
      timeline.add({
        targets: '#' + idTarget,
        boxShadow: '0px 0px 15px rgba(' + blue + ', 0.4)',
      })

      return timeline
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
