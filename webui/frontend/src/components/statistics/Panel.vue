<template>
  <v-col cols='12' lg='6'>
    <v-card>
      <v-card-title>
        {{ title }}
      </v-card-title>
      <!-- <v-divider/> -->
      <v-card-text>
        <div v-for="item in items" :key="item.title" class="text-left">
          <span>
            {{ item.title }}:
            <span class="text-info font-weight-bold">{{ item.value }}</span>
          </span>
        </div>
      </v-card-text>
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
      showDetailsBtnID: null,
      show: false
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
