<template>
  <v-container class="my-4">
    <v-row justify='center' class="mx-5 pa-4">
      <v-col
        cols="7"
        sm="4"
        lg="3"
        xl="2"
        class="ma-3 mx-md-4 mx-lg-5 py-4"
      >
        <div id="atcBox">
          <v-card elevation='6' class="pa-1">
            <v-card-title class="d-flex justify-center">
              Active
            </v-card-title>
            <v-card-subtitle class="display-4 text-center my-1">
              {{ activeTasksCounter }}
            </v-card-subtitle>
          </v-card>
        </div>
      </v-col>

      <v-col
        cols="7"
        sm="4"
        lg="3"
        xl="2"
        class="ma-3 mx-md-4 mx-lg-5 py-4"
      >
        <div id="oetcBox">
          <v-card elevation='6' class="pa-1">
            <v-card-title class="d-flex justify-center">
              Running...
            </v-card-title>
            <v-card-subtitle class="display-4 text-center my-1">
              {{ onExecutionTasksCounter }}
            </v-card-subtitle>
          </v-card>
        </div>
      </v-col>

      <v-col
        cols="7"
        sm="4"
        lg="3"
        xl="2"
        class="ma-3 mx-md-4 mx-lg-5 py-4"
      >
        <div id="itcBox">
          <v-card elevation='6' class="pa-1">
            <v-card-title class="d-flex justify-center">
              Inactive
            </v-card-title>
            <v-card-subtitle class="display-4 text-center my-1">
              {{ inactiveTasksCounter }}
            </v-card-subtitle>
          </v-card>
        </div>
      </v-col>
    </v-row>
  </v-container>
</template>

<script>
import anime from 'animejs'
import { mapGetters } from 'vuex'

export default {
  data () {
    return {
      atcAnimation: null,
      oetcAnimation: null,
      itcAnimation: null
    }
  },
  computed: {
    ...mapGetters('statistics', [
      'activeTasksCounter',
      'onExecutionTasksCounter',
      'inactiveTasksCounter'
    ])
  },
  watch: {
    activeTasksCounter: function (newValue) {
      if (this.atcAnimation != null) {
        this.atcAnimation.restart()
      } else {
        this.atcAnimation = this.boxAnimation('atcBox')
      }
    },
    onExecutionTasksCounter: function (newValue) {
      if (this.oetcAnimation != null) {
        this.oetcAnimation.restart()
      } else {
        this.oetcAnimation = this.boxAnimation('oetcBox')
      }
    },
    inactiveTasksCounter: function (newValue) {
      if (this.itcAnimation != null) {
        this.itcAnimation.restart()
      } else {
        this.itcAnimation = this.boxAnimation('itcBox')
      }
    }
  },
  methods: {
    boxAnimation (targetID) {
      const blue = '48, 170, 232'

      const timeline = anime.timeline({ easing: 'linear', direction: 'alternate' })
      timeline.add({
        targets: '#' + targetID,
        boxShadow: '0px 0px 15px rgba(' + blue + ', 0.4)'
      })

      return timeline
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
