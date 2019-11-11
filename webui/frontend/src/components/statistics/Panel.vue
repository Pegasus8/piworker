<template>
  <b-row class="m-3">
    <b-col class="">
      <b-card no-body bg-variant="light" border-variant="light">
        <b-card-body class="text-center">
          <span class="card-title h5">{{ title }}</span>
        </b-card-body>
        <b-collapse :id="'collapsePanel' + panelID">
          <b-card class="px-sm-5 text-truncate">
            <ul>
              <li v-for="item in items" :key="item.title">
                <span class="text-secondary">
                  {{ item.title }}: <span class="text-info font-weight-bold">{{ item.value }}</span>
                </span>
              </li>
            </ul>
          </b-card>
        </b-collapse>
        <div class="text-center">
          <transition name="rotate" mode="out-in">
            <span
              v-if="!showDetails"
              class="icon-circle-down text-muted"
              v-b-toggle="'collapsePanel' + panelID"
              @click="showDetails = true"
            ></span>
            <span
              v-else
              class="icon-circle-up text-muted"
              v-b-toggle="'collapsePanel' + panelID"
              @click="showDetails = false"
            ></span>
          </transition>
        </div>
      </b-card>
    </b-col>
  </b-row>
</template>

<script>
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
  data() {
    return {
      panelID: null,
      showDetails: false
    }
  },
  mounted() {
    this.panelID = this._uid;
  }
};
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}

.rotate-enter{
}

.rotate-enter-active{
  -webkit-transition-duration: 1s;
  -moz-transition-duration: 1s;
  -o-transition-duration: 1s;
  transition-duration: 1s;
  -webkit-transition-property: -webkit-transform;
  -moz-transition-property: -moz-transform;
  -o-transition-property: -o-transform;
  transition-property: transform;
  transform: rotate(180deg) !important;
}

.rotate-leave{
}

.rotate-leave-active{
  -webkit-transition-duration: 1s;
  -moz-transition-duration: 1s;
  -o-transition-duration: 1s;
  transition-duration: 1s;
  -webkit-transition-property: -webkit-transform;
  -moz-transition-property: -moz-transform;
  -o-transition-property: -o-transform;
  transition-property: transform;
}
</style>
