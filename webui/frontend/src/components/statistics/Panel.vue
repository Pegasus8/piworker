<template>
  <v-col cols='12' lg='6'>
    <v-card>
      <v-card-text>
        <h5 class="text-center">{{ title }}</h5>
      </v-card-text>
      <b-collapse :id="'collapsePanel' + panelID">
        <v-card elevation='1' class="px-sm-5 text-truncate">
          <ul>
            <li v-for="item in items" :key="item.title">
              <span class="text-secondary">
                {{ item.title }}:
                <span class="text-info font-weight-bold">{{ item.value }}</span>
              </span>
            </li>
          </ul>
        </v-card>
      </b-collapse>
      <div>
        <div
          class="icon-circle-down text-muted"
          :id="showDetailsBtnID"
          v-b-toggle="'collapsePanel' + panelID"
          @click="showDetailsBtn"
        ></div>
      </div>
    </v-card>
  </v-col>
</template>

<script>
import anime from 'animejs'
export default {
  props: {
    title: {
      type: String,
      required: true
    },
    items: {
      type: Array,
      required: true
    }
  },
  data () {
    return {
      panelID: null,
      showDetails: false,
      showDetailsBtnID: null
    }
  },
  methods: {
    showDetailsBtn () {
      this.showDetails = !this.showDetails

      let rotation = 0
      if (this.showDetails) {
        rotation = 180
      }
      anime({
        targets: '#' + this.showDetailsBtnID,
        rotate: rotation,
        duration: 500,
        easing: 'easeInOutQuad'
      })
    }
  },
  mounted () {
    this.panelID = this._uid
    this.showDetailsBtnID = 'showDetails' + this._uid
  }
}
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
</style>
