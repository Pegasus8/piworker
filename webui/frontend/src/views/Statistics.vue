<template>
  <div>
    <div class='d-flex flex-row-reverse'>
      <v-btn :to='{ name: "detailed-statistics" }' small text>
        Detailed statistics >>
      </v-btn>
    </div>
    <app-boxes></app-boxes>
    <app-panels></app-panels>
  </div>
</template>

<script>
import Boxes from '../components/statistics/Boxes.vue'
import Panels from '../components/statistics/Panels.vue'
const isEmpty = require('lodash.isempty')

export default {
  components: {
    appBoxes: Boxes,
    appPanels: Panels
  },
  mounted () {
    this.$websocket.connect()

    if (!this.$store.getters['auth/isAuthenticated']) {
      return
    }

    if (isEmpty(this.$store.getters['elementsInfo/triggers'])) {
      this.$store.dispatch('elementsInfo/updateTriggersInfo')
        .catch(err => console.error(err)) // TODO Handle the error correctly.
    }

    if (isEmpty(this.$store.getters['elementsInfo/actions'])) {
      this.$store.dispatch('elementsInfo/updateActionsInfo')
        .catch(err => console.error(err)) // TODO Handle the error correctly.
    }

    if (isEmpty(this.$store.getters['elementsInfo/typesCompat'])) {
      this.$store.dispatch('elementsInfo/getTypesCompatList')
        .catch(err => console.error(err)) // TODO Handle the error correctly.
    }

    if (isEmpty(this.$store.getters['userTasks/tasks'])) {
      this.$store.dispatch('userTasks/fetchUserTasks')
    }

    if (isEmpty(this.$store.getters['statistics/date'])) {
      let today = new Date()
      const dd = String(today.getDate()).padStart(2, '0')
      const mm = String(today.getMonth() + 1).padStart(2, '0')
      const yyyy = today.getFullYear()
      today = yyyy + '-' + mm + '-' + dd

      this.$store.dispatch('statistics/setDate', today)
      this.$store.dispatch('statistics/getStats', { date: today })
    }
  },
  beforeDestroy () {
    this.$websocket.disconnect()
  }
}
</script>
